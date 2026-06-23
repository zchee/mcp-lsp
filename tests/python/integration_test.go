// Copyright 2026 The mcp-lsp Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package integration drives the mcp-lsp command end to end against a real
// pyright language server. It builds the command, materializes a fixture
// project from a txtar archive, and connects as a real MCP client over a
// subprocess transport, so it exercises the same path an agent uses rather than
// the in-memory fakes the mcp package unit tests rely on.
package python_integration_test

import (
	"context"
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-json-experiment/json"
	gocmp "github.com/google/go-cmp/cmp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/tools/txtar"
)

// integrationEnv gates the live integration test. It drives the real mcp-lsp
// command against a real pyright over a subprocess MCP transport, so it is
// opt-in and excluded from a plain `go test ./...`.
const integrationEnv = "MCP_LSP_INTEGRATION"

// serverCommand is the pyright LSP entrypoint and its stdio flag. pyright-
// langserver speaks LSP only when invoked with --stdio; without it the process
// exits immediately complaining that no connection stream is set.
var serverCommand = []string{"pyright-langserver", "--stdio"}

// fixtureArchive is the fixture project the test materializes to disk. Keeping
// it as a txtar archive separates the test data from the test logic and lets the
// fixture diff cleanly in review rather than living as escaped Go strings.
//
//go:embed testdata/fixture.txtar
var fixtureArchive []byte

// diagnostic mirrors the exported fields of the lsp_diagnostics tool output a
// caller asserts on. It is decoded from the structured tool result rather than
// imported from the mcp package so the test reads the same JSON an agent does.
type diagnostic struct {
	Line     uint32 `json:"line"`
	Column   uint32 `json:"column"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// diagnosticResult is the subset of the tool's structured output the test
// inspects: the model that produced it and the diagnostics themselves.
type diagnosticResult struct {
	Mode        string       `json:"mode"`
	Diagnostics []diagnostic `json:"diagnostics"`
}

// TestIntegrationPyright drives the mcp-lsp -serve command as a real MCP client,
// over a subprocess stdio transport, against a live pyright. It proves the full
// path an agent uses: the MCP handshake, tools/list, and a tools/call that
// reaches pyright through the LSP client and returns structured diagnostics.
//
// The test is opt-in: it builds the command, writes a fixture project to disk,
// and spawns pyright, so it is skipped unless MCP_LSP_INTEGRATION is set and
// pyright-langserver is on PATH. It is also skipped under -short.
func TestIntegrationPyright(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test under -short")
	}
	if os.Getenv(integrationEnv) == "" {
		t.Skipf("set %s=1 to run the live pyright integration test", integrationEnv)
	}
	if _, err := exec.LookPath(serverCommand[0]); err != nil {
		t.Skipf("%s not found on PATH: %v", serverCommand[0], err)
	}

	bin := buildCommand(t)
	fixture := writeFixtureProject(t)

	// A real MCP client over a subprocess transport: CommandTransport spawns the
	// server, keeps stdin open for the whole session, and closes it on teardown,
	// which is what distinguishes a live client from piping a static request file.
	args := append([]string{"-serve", "--"}, serverCommand...)
	cmd := exec.Command(bin, args...)
	cmd.Dir = fixture // relative tool paths resolve against the server working directory.

	client := mcp.NewClient(&mcp.Implementation{Name: "mcp-lsp-itest", Version: "0.0.1"}, nil)

	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()

	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect to mcp-lsp server: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if !hasTool(tools, "lsp_diagnostics") {
		t.Fatalf("lsp_diagnostics not advertised; got %d tool(s)", len(tools.Tools))
	}

	// broken.py references an undefined name, so pyright type-checking must report
	// exactly one error-severity diagnostic at its position. clean.py type-checks,
	// so both models must report none. The position is one-based to match how the
	// tool flattens LSP ranges.
	wantBroken := []diagnostic{{
		Line:     2,
		Column:   12,
		Severity: "error",
		Message:  `"undefined_symbol" is not defined`,
	}}

	tests := map[string]struct {
		file string
		mode string
		want []diagnostic
	}{
		"error: pull reports the undefined name": {
			file: "broken.py",
			mode: "pull",
			want: wantBroken,
		},
		"error: push reports the undefined name": {
			file: "broken.py",
			mode: "push",
			want: wantBroken,
		},
		"success: pull reports a clean file as empty": {
			file: "clean.py",
			mode: "pull",
			want: nil,
		},
		"success: push reports a clean file as empty": {
			file: "clean.py",
			mode: "push",
			want: nil,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			res, err := session.CallTool(ctx, &mcp.CallToolParams{
				Name: "lsp_diagnostics",
				Arguments: map[string]any{
					"file":     tt.file,
					"language": "python",
					"mode":     tt.mode,
				},
			})
			if err != nil {
				t.Fatalf("tools/call: %v", err)
			}
			if res.IsError {
				t.Fatalf("tool returned error: %v", toolErrorText(res))
			}

			got := decodeStructured(t, res)
			if got.Mode != tt.mode {
				t.Errorf("mode = %q, want %q", got.Mode, tt.mode)
			}
			if diff := gocmp.Diff(tt.want, got.Diagnostics, cmpEmptySlice()); diff != "" {
				t.Errorf("diagnostics mismatch (-want +got):\n%s", diff)
			}
		})
	}

	// An unknown mode is rejected by the handler and must surface to the client as
	// a tool error rather than a transport failure, so an agent can correct the
	// call instead of treating the session as broken.
	t.Run("error: an unknown mode is reported as a tool error", func(t *testing.T) {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "lsp_diagnostics",
			Arguments: map[string]any{
				"file": "clean.py",
				"mode": "sideways",
			},
		})
		if err != nil {
			t.Fatalf("tools/call: %v", err)
		}
		if !res.IsError {
			t.Fatal("expected a tool error for an unknown mode, got success")
		}
	})
}

// buildCommand compiles the mcp-lsp command into a temporary directory and
// returns the binary path, so the test exercises a freshly built server rather
// than depending on one being pre-installed. The command's main package is the
// module root, two directories above this test.
func buildCommand(t *testing.T) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "mcp-lsp")
	cmd := exec.Command("go", "build", "-o", bin, "../..")
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building mcp-lsp command: %v\n%s", err, out)
	}
	return bin
}

// writeFixtureProject materializes the embedded txtar fixture into a temporary
// directory and returns its path. pyright type-checks files on disk, so the
// archive's files are written out rather than served from an in-memory fs.FS.
func writeFixtureProject(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	for _, f := range txtar.Parse(fixtureArchive).Files {
		path := filepath.Join(dir, filepath.FromSlash(f.Name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("creating fixture dir for %s: %v", f.Name, err)
		}
		if err := os.WriteFile(path, f.Data, 0o600); err != nil {
			t.Fatalf("writing fixture %s: %v", f.Name, err)
		}
	}
	return dir
}

// hasTool reports whether the tool list advertises a tool with the given name.
func hasTool(tools *mcp.ListToolsResult, name string) bool {
	for _, tool := range tools.Tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

// toolErrorText concatenates the text content of a tool-error result so a
// failing test can report the message the handler returned.
func toolErrorText(res *mcp.CallToolResult) string {
	var s string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			s += tc.Text
		}
	}
	return s
}

// decodeStructured re-marshals the structured tool result and decodes it into
// the test's view of the output, mirroring how an agent reads structuredContent.
func decodeStructured(t *testing.T, res *mcp.CallToolResult) diagnosticResult {
	t.Helper()

	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshalling structured content: %v", err)
	}
	var out diagnosticResult
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decoding structured content: %v", err)
	}
	return out
}

// cmpEmptySlice treats a nil and an empty diagnostic slice as equal, since a
// clean file yields a non-nil empty slice from the tool while the test declares
// the wanted result as nil.
func cmpEmptySlice() gocmp.Option {
	return gocmp.FilterValues(
		func(x, y []diagnostic) bool { return len(x) == 0 && len(y) == 0 },
		gocmp.Comparer(func([]diagnostic, []diagnostic) bool { return true }),
	)
}
