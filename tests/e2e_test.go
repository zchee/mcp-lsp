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

// Package tests contains end-to-end tests that exercise the built mcp-lsp
// binary against a real language server. They are gated behind the
// MCP_LSP_INTEGRATION environment variable and skipped when gopls is absent.
package tests

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// errorFixture is a Go source file with a deliberate compile error: it calls an
// undeclared function, which gopls reports as an error diagnostic.
const errorFixture = `package main

func main() {
	undeclaredFunction()
}
`

func TestE2EDiagnostics(t *testing.T) {
	if os.Getenv("MCP_LSP_INTEGRATION") == "" {
		t.Skip("set MCP_LSP_INTEGRATION=1 to run the end-to-end tests")
	}
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not found on PATH; skipping the end-to-end test")
	}

	bin := buildBinary(t)
	workspace := newWorkspace(t)
	fixture := filepath.Join(workspace, "main.go")

	ctx := t.Context()

	cmd := exec.CommandContext(ctx, bin, "-workspace", workspace, "-log-level", "error")
	cmd.Stderr = os.Stderr
	transport := &mcp.CommandTransport{Command: cmd}

	client := mcp.NewClient(&mcp.Implementation{Name: "e2e-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect to mcp-lsp: %v", err)
	}
	defer func() { _ = session.Close() }()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "lsp_diagnostics",
		Arguments: map[string]any{
			"file":     fixture,
			"language": "go",
		},
	})
	if err != nil {
		t.Fatalf("call lsp_diagnostics: %v", err)
	}
	if res.IsError {
		t.Fatalf("lsp_diagnostics returned a tool error: %+v", res.Content)
	}

	out := decodeOutput(t, res)
	if len(out.Diagnostics) == 0 {
		t.Fatalf("expected at least one diagnostic for a file with a compile error, got none")
	}

	var foundError bool
	for _, d := range out.Diagnostics {
		if d.Severity != "error" {
			continue
		}
		foundError = true
		if d.StartLine < 1 || d.StartColumn < 1 {
			t.Errorf("diagnostic positions are not one-based: %+v", d)
		}
	}
	if !foundError {
		t.Errorf("no error-severity diagnostic reported; diagnostics = %+v", out.Diagnostics)
	}
}

// diagnosticItem mirrors the tool's output item for decoding the structured
// result without importing the server package.
type diagnosticItem struct {
	Severity    string `json:"severity"`
	Message     string `json:"message"`
	Source      string `json:"source"`
	Code        string `json:"code"`
	StartLine   int    `json:"startLine"`
	StartColumn int    `json:"startColumn"`
	EndLine     int    `json:"endLine"`
	EndColumn   int    `json:"endColumn"`
}

type diagnosticsOutput struct {
	File        string           `json:"file"`
	Diagnostics []diagnosticItem `json:"diagnostics"`
}

// decodeOutput extracts the structured tool output from a call result.
func decodeOutput(t *testing.T, res *mcp.CallToolResult) diagnosticsOutput {
	t.Helper()

	if res.StructuredContent == nil {
		t.Fatal("tool result has no structured content")
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var out diagnosticsOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal structured content: %v", err)
	}

	return out
}

// buildBinary compiles the mcp-lsp binary into a temp dir and returns its path.
func buildBinary(t *testing.T) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "mcp-lsp")
	cmd := exec.CommandContext(t.Context(), "go", "build", "-o", bin, ".")
	cmd.Dir = repoRoot(t)
	cmd.Env = os.Environ()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build mcp-lsp: %v\n%s", err, out)
	}

	return bin
}

// newWorkspace creates a temporary Go module workspace with a fixture that has a
// compile error, and waits briefly so the file modification time is stable.
func newWorkspace(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module e2e\n\ngo 1.27\n")
	writeFile(t, filepath.Join(dir, "main.go"), errorFixture)

	// Give the filesystem a moment so gopls observes a settled workspace.
	time.Sleep(100 * time.Millisecond)

	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// repoRoot returns the module root by walking up from the test file directory
// until it finds go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repo root (go.mod) from the test directory")
		}
		dir = parent
	}
}
