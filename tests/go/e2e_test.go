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

package gointegration

import (
	"fmt"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.lsp.dev/uri"

	mcpserver "github.com/zchee/mcp-lsp/pkg/mcp"
	"github.com/zchee/mcp-lsp/tests/internal/lsptest"
)

// These binary-level tests live beside the Go/gopls integration tests because
// they use the same Go fixtures, but they exercise a different boundary: the
// compiled mcp-lsp server over MCP stdio rather than pkg/lsp directly.
func TestE2EDiagnostics(t *testing.T) {
	requireIntegration(t)

	ws := extractFixture(t, "diagnostics_error.txtar")
	fixture := ws.Path("main.go")
	session := lsptest.NewE2ESession(t, ws.Dir())

	res, err := session.CallTool(t.Context(), &mcpsdk.CallToolParams{
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

	out := lsptest.DecodeStructured[mcpserver.DiagnosticsOutput](t, res)
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

func TestE2EDefinition(t *testing.T) {
	requireIntegration(t)

	ws := extractFixture(t, "definition_local.txtar")
	fixture := ws.Path("main.go")
	query := ws.MarkerPosition(t, "main.go", "query", "answer")
	target := ws.MarkerPosition(t, "main.go", "target", "answer")
	targetURI := string(uri.File(fixture))
	targetLine := int(target.Line) + 1
	targetColumn := int(target.Character) + 1
	session := lsptest.NewE2ESession(t, ws.Dir())

	var out mcpserver.DefinitionOutput
	var lastErr error
	for range goDefinitionLookup.Attempts {
		res, err := session.CallTool(t.Context(), &mcpsdk.CallToolParams{
			Name: "lsp_definition",
			Arguments: map[string]any{
				"file":     fixture,
				"line":     int(query.Line) + 1,
				"column":   int(query.Character) + 1,
				"language": "go",
			},
		})
		if err != nil {
			lastErr = err
			waitForDefinition(t)
			continue
		}
		if res.IsError {
			lastErr = fmt.Errorf("lsp_definition returned a tool error: %+v", res.Content)
			waitForDefinition(t)
			continue
		}
		out = lsptest.DecodeStructured[mcpserver.DefinitionOutput](t, res)
		if len(out.Definitions) > 0 {
			break
		}
		waitForDefinition(t)
	}
	if len(out.Definitions) == 0 {
		t.Fatalf("expected at least one definition, got none; last error = %v, output = %+v", lastErr, out)
	}

	for _, def := range out.Definitions {
		if def.TargetURI != targetURI {
			continue
		}
		if def.TargetSelectionRange.StartLine == targetLine && def.TargetSelectionRange.StartColumn == targetColumn {
			return
		}
	}
	t.Fatalf("no definition pointed to %s at %d:%d; definitions = %+v", targetURI, targetLine, targetColumn, out.Definitions)
}

func waitForDefinition(t *testing.T) {
	t.Helper()

	if err := lsptest.SleepOrCancel(t.Context(), goDefinitionLookup.RetryDelay); err != nil {
		t.Fatalf("context canceled while waiting for %s: %v", goDefinitionLookup.ServerName, err)
	}
}
