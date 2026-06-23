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

// Package lsptest is the shared harness for the per-language integration tests
// under tests/. Each language test embeds its own fixture archive, declares a
// [Config] naming the language server and the cases to run, and calls [Run];
// the harness owns everything those tests have in common: opt-in gating,
// building the mcp-lsp command, materializing the txtar fixture, connecting as
// a real MCP client over a subprocess transport, and asserting the
// lsp_diagnostics output. The per-language files stay down to their genuine
// differences — the server command, the language identifier, and the cases.
package lsptest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-json-experiment/json"
	gocmp "github.com/google/go-cmp/cmp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/tools/txtar"
)

// integrationEnv gates every integration test. Each one drives the real mcp-lsp
// command against a real language server over a subprocess MCP transport, so it
// is opt-in and excluded from a plain `go test ./...`.
const integrationEnv = "MCP_LSP_INTEGRATION"

// toolName is the MCP tool every case calls.
const toolName = "lsp_diagnostics"

// defaultTimeout bounds a session when a [Config] does not set its own; it is
// generous enough for a cold language server to reach first analysis.
const defaultTimeout = 90 * time.Second

// Diagnostic is the flattened form of a single LSP diagnostic a case asserts on.
// It is decoded from the structured tool result rather than imported from the
// mcp package so the test reads the same JSON an agent does. Positions are
// one-based, matching how the tool flattens LSP ranges.
type Diagnostic struct {
	Line     uint32 `json:"line"`
	Column   uint32 `json:"column"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// Case is one lsp_diagnostics call and its expected result. WaitSeconds, when
// positive, sets the push-model wait budget for servers that need longer than
// the tool's default to publish diagnostics; it is omitted from the call
// otherwise. Want is the expected diagnostic set, nil for a clean file.
type Case struct {
	File        string
	Mode        string
	WaitSeconds int
	Want        []Diagnostic
}

// Config describes one language's integration test: the language server to
// spawn, the LSP language identifier, the embedded txtar fixture to materialize,
// the named cases to run, and an optional session timeout (defaulting to
// [defaultTimeout]).
type Config struct {
	// Server is the language server command and its arguments, e.g.
	// {"gopls", "serve"} or {"pyright-langserver", "--stdio"}.
	Server []string

	// Language is the LSP language identifier passed to the tool, e.g. "go".
	Language string

	// Fixture is the embedded txtar archive materialized to the workspace.
	Fixture []byte

	// Timeout bounds the session; zero means [defaultTimeout].
	Timeout time.Duration

	// Cases are the lsp_diagnostics calls to run, keyed by subtest name.
	Cases map[string]Case
}

// Run executes the integration test described by cfg. It skips unless
// MCP_LSP_INTEGRATION is set and the server is on PATH, and under -short. It
// builds the mcp-lsp command, materializes the fixture, connects as a real MCP
// client over a subprocess stdio transport, runs every case as a subtest, and
// finally asserts that an unknown mode surfaces as a tool error rather than a
// transport failure.
func Run(t *testing.T, cfg *Config) {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping integration test under -short")
	}
	if os.Getenv(integrationEnv) == "" {
		t.Skipf("set %s=1 to run the live %s integration test", integrationEnv, cfg.Server[0])
	}
	if _, err := exec.LookPath(cfg.Server[0]); err != nil {
		t.Skipf("%s not found on PATH: %v", cfg.Server[0], err)
	}

	bin := buildCommand(t)
	workspace := writeFixture(t, cfg.Fixture)

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()

	// A real MCP client over a subprocess transport: CommandTransport spawns the
	// server, keeps stdin open for the whole session, and closes it on teardown,
	// which is what distinguishes a live client from piping a static request file.
	args := append([]string{"-serve", "--"}, cfg.Server...)
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = workspace // relative tool paths resolve against the server working directory.

	client := mcp.NewClient(&mcp.Implementation{Name: "mcp-lsp-itest", Version: "0.0.1"}, nil)

	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect to mcp-lsp server: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if !hasTool(tools, toolName) {
		t.Fatalf("%s not advertised; got %d tool(s)", toolName, len(tools.Tools))
	}

	for name, tc := range cfg.Cases {
		t.Run(name, func(t *testing.T) {
			arguments := map[string]any{
				"file":     tc.File,
				"language": cfg.Language,
				"mode":     tc.Mode,
			}
			if tc.WaitSeconds > 0 {
				arguments["waitSeconds"] = tc.WaitSeconds
			}
			res, err := session.CallTool(ctx, &mcp.CallToolParams{
				Name:      toolName,
				Arguments: arguments,
			})
			if err != nil {
				t.Fatalf("tools/call: %v", err)
			}
			if res.IsError {
				t.Fatalf("tool returned error: %v", toolErrorText(res))
			}

			got := decodeStructured(t, res)
			if got.Mode != tc.Mode {
				t.Errorf("mode = %q, want %q", got.Mode, tc.Mode)
			}
			if diff := gocmp.Diff(tc.Want, got.Diagnostics, cmpEmptySlice()); diff != "" {
				t.Errorf("diagnostics mismatch (-want +got):\n%s", diff)
			}
		})
	}

	// An unknown mode is rejected by the handler and must surface to the client as
	// a tool error rather than a transport failure, so an agent can correct the
	// call instead of treating the session as broken. Any case's file is a valid
	// target for this check, so reuse the first one.
	t.Run("error: an unknown mode is reported as a tool error", func(t *testing.T) {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: toolName,
			Arguments: map[string]any{
				"file": anyCaseFile(cfg.Cases),
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

// diagnosticResult is the subset of the tool's structured output the harness
// inspects: the model that produced it and the diagnostics themselves.
type diagnosticResult struct {
	Mode        string       `json:"mode"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// commandPackage is the import path of the mcp-lsp command, built by path rather
// than by a relative directory so the build is independent of which test
// package's working directory go test runs it from.
const commandPackage = "github.com/zchee/mcp-lsp"

// buildCommand compiles the mcp-lsp command into a temporary directory and
// returns the binary path, so the test exercises a freshly built server rather
// than depending on one being pre-installed.
func buildCommand(t *testing.T) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "mcp-lsp")
	cmd := exec.CommandContext(t.Context(), "go", "build", "-o", bin, commandPackage)
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building mcp-lsp command: %v\n%s", err, out)
	}
	return bin
}

// writeFixture materializes a txtar archive into a temporary directory and
// returns its path. Language servers analyze files on disk, so the archive's
// files are written out rather than served from an in-memory fs.FS.
func writeFixture(t *testing.T, archive []byte) string {
	t.Helper()

	dir := t.TempDir()
	for _, f := range txtar.Parse(archive).Files {
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

// anyCaseFile returns the file of an arbitrary case, used as a valid target for
// the unknown-mode error check. Map order is unspecified, which is fine: every
// case names a file the server can open.
func anyCaseFile(cases map[string]Case) string {
	for _, tc := range cases {
		return tc.File
	}
	return ""
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
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// decodeStructured re-marshals the structured tool result and decodes it into
// the harness's view of the output, mirroring how an agent reads
// structuredContent.
func decodeStructured(t *testing.T, res *mcp.CallToolResult) diagnosticResult {
	t.Helper()

	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshaling structured content: %v", err)
	}
	var out diagnosticResult
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decoding structured content: %v", err)
	}
	return out
}

// cmpEmptySlice treats a nil and an empty diagnostic slice as equal, since a
// clean file yields a non-nil empty slice from the tool while a case declares
// the wanted result as nil.
func cmpEmptySlice() gocmp.Option {
	return gocmp.FilterValues(
		func(x, y []Diagnostic) bool { return len(x) == 0 && len(y) == 0 },
		gocmp.Comparer(func([]Diagnostic, []Diagnostic) bool { return true }),
	)
}
