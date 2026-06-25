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

package lsptest

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewE2ESession builds the mcp-lsp binary, starts it with workspace as its
// workspace root, and returns an MCP client session connected over stdio.
func NewE2ESession(t *testing.T, workspace string) *mcp.ClientSession {
	t.Helper()

	bin := BuildBinary(t)
	cmd := exec.CommandContext(t.Context(), bin, "-workspace", workspace, "-log-level", "error")
	cmd.Stderr = os.Stderr
	transport := &mcp.CommandTransport{Command: cmd}

	client := mcp.NewClient(&mcp.Implementation{Name: "e2e-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(t.Context(), transport, nil)
	if err != nil {
		t.Fatalf("connect to mcp-lsp: %v", err)
	}
	CleanupSession(t, session.Close)
	return session
}

// BuildBinary compiles the mcp-lsp binary into a temporary directory and
// returns its path.
func BuildBinary(t *testing.T) string {
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

// DecodeStructured extracts structured tool output from an [mcp.CallToolResult].
func DecodeStructured[T any](t *testing.T, res *mcp.CallToolResult) T {
	t.Helper()

	if res.StructuredContent == nil {
		t.Fatal("tool result has no structured content")
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal structured content: %v", err)
	}
	return out
}

// CleanupSession closes CommandTransport-backed MCP sessions. The Go SDK
// returns the subprocess wait status during forced shutdown, commonly
// "signal: killed", so cleanup logs that transport artifact instead of failing
// assertions that already verified protocol behavior.
func CleanupSession(t *testing.T, closeSession func() error) {
	t.Helper()

	t.Cleanup(func() {
		if err := closeSession(); err != nil {
			t.Logf("close MCP session after assertions: %v", err)
		}
	})
}

// repoRoot returns the module root by walking up from the test package
// directory until it finds go.mod.
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
