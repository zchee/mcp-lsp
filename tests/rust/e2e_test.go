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

package rustintegration

import (
	"fmt"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.lsp.dev/uri"

	mcpserver "github.com/zchee/mcp-lsp/pkg/mcp"
	"github.com/zchee/mcp-lsp/tests/internal/lsptest"
)

func TestE2ERustAnalyzerDiagnosticsReportsCompileError(t *testing.T) {
	requireIntegration(t)

	ws := extractFixture(t, "diagnostics_error.txtar")
	fixture := ws.Path("src/main.rs")
	session := lsptest.NewE2ESession(t, ws.Dir())

	out := callRustDiagnosticsTool(t, session, fixture)

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

// TestE2ERustAnalyzerDefinition exercises the compiled mcp-lsp binary over MCP
// stdio, proving the public default registry can route Rust definition requests
// to rust-analyzer without using the test-local Manager configuration.
func TestE2ERustAnalyzerDefinition(t *testing.T) {
	requireIntegration(t)

	ws := extractFixture(t, "definition_crossfile.txtar")
	fixture := ws.Path("src/main.rs")
	query := ws.MarkerPosition(t, "src/main.rs", "query", "Greeting")
	target := ws.MarkerPosition(t, "src/lib.rs", "target", "Greeting")
	targetURI := string(uri.File(ws.Path("src/lib.rs")))
	targetLine := int(target.Line) + 1
	targetColumn := int(target.Character) + 1
	session := lsptest.NewE2ESession(t, ws.Dir())

	var out mcpserver.DefinitionOutput
	var lastErr error
	for range rustDefinitionLookup.Attempts {
		res, err := session.CallTool(t.Context(), &mcpsdk.CallToolParams{
			Name: "lsp_definition",
			Arguments: map[string]any{
				"file":     fixture,
				"line":     int(query.Line) + 1,
				"column":   int(query.Character) + 1,
				"language": rustDefinitionLookup.Language,
			},
		})
		if err != nil {
			lastErr = err
			waitForRustDefinition(t)
			continue
		}
		if res.IsError {
			lastErr = fmt.Errorf("lsp_definition returned a tool error: %+v", res.Content)
			waitForRustDefinition(t)
			continue
		}
		out = lsptest.DecodeStructured[mcpserver.DefinitionOutput](t, res)
		if len(out.Definitions) > 0 {
			break
		}
		waitForRustDefinition(t)
	}
	if len(out.Definitions) == 0 {
		t.Fatalf("expected at least one Rust definition, got none; last error = %v, output = %+v", lastErr, out)
	}

	for _, def := range out.Definitions {
		if def.TargetURI != targetURI {
			continue
		}
		if def.TargetSelectionRange.StartLine == targetLine && def.TargetSelectionRange.StartColumn == targetColumn {
			return
		}
	}
	t.Fatalf("no Rust definition pointed to %s at %d:%d; definitions = %+v", targetURI, targetLine, targetColumn, out.Definitions)
}

func TestE2ERustAnalyzerImplementation(t *testing.T) {
	requireIntegration(t)

	ws := extractFixture(t, "implementation_trait.txtar")
	fixture := ws.Path("src/main.rs")
	query := ws.MarkerPosition(t, "src/main.rs", "query", "greet")
	target := ws.MarkerPosition(t, "src/main.rs", "target", "greet")
	targetURI := string(uri.File(fixture))
	targetLine := int(target.Line) + 1
	targetColumn := int(target.Character) + 1
	session := lsptest.NewE2ESession(t, ws.Dir())

	var out mcpserver.ImplementationOutput
	var lastErr error
	for range rustImplementationLookup.Attempts {
		res, err := session.CallTool(t.Context(), &mcpsdk.CallToolParams{
			Name: "lsp_implementation",
			Arguments: map[string]any{
				"file":     fixture,
				"line":     int(query.Line) + 1,
				"column":   int(query.Character) + 1,
				"language": rustImplementationLookup.Language,
			},
		})
		if err != nil {
			lastErr = err
			waitForRustImplementation(t)
			continue
		}
		if res.IsError {
			lastErr = fmt.Errorf("lsp_implementation returned a tool error: %+v", res.Content)
			waitForRustImplementation(t)
			continue
		}
		out = lsptest.DecodeStructured[mcpserver.ImplementationOutput](t, res)
		if len(out.Implementations) > 0 {
			break
		}
		waitForRustImplementation(t)
	}
	if len(out.Implementations) == 0 {
		t.Fatalf("expected at least one Rust implementation, got none; last error = %v, output = %+v", lastErr, out)
	}

	for _, implementation := range out.Implementations {
		if implementation.TargetURI != targetURI {
			continue
		}
		if implementation.TargetSelectionRange.StartLine == targetLine && implementation.TargetSelectionRange.StartColumn == targetColumn {
			return
		}
	}
	t.Fatalf("no Rust implementation pointed to %s at %d:%d; implementations = %+v", targetURI, targetLine, targetColumn, out.Implementations)
}

func waitForRustDefinition(t *testing.T) {
	t.Helper()

	if err := lsptest.SleepOrCancel(t.Context(), rustDefinitionLookup.RetryDelay); err != nil {
		t.Fatalf("context canceled while waiting for %s: %v", rustDefinitionLookup.ServerName, err)
	}
}

func waitForRustImplementation(t *testing.T) {
	t.Helper()

	if err := lsptest.SleepOrCancel(t.Context(), rustImplementationLookup.RetryDelay); err != nil {
		t.Fatalf("context canceled while waiting for %s: %v", rustImplementationLookup.ServerName, err)
	}
}

func callRustDiagnosticsTool(t *testing.T, session *mcpsdk.ClientSession, fixture string) mcpserver.DiagnosticsOutput {
	t.Helper()

	var (
		out     mcpserver.DiagnosticsOutput
		lastErr error
	)
	for range rustDiagnosticsLookup.Attempts {
		res, err := session.CallTool(t.Context(), &mcpsdk.CallToolParams{
			Name: "lsp_diagnostics",
			Arguments: map[string]any{
				"file":     fixture,
				"language": rustDiagnosticsLookup.Language,
			},
		})
		if err != nil {
			lastErr = err
			waitForRustDiagnostics(t)
			continue
		}
		if res.IsError {
			lastErr = fmt.Errorf("lsp_diagnostics returned a tool error: %+v", res.Content)
			waitForRustDiagnostics(t)
			continue
		}
		out = lsptest.DecodeStructured[mcpserver.DiagnosticsOutput](t, res)
		if len(out.Diagnostics) > 0 {
			return out
		}
		waitForRustDiagnostics(t)
	}
	t.Fatalf("expected at least one diagnostic for a Rust file with a compile error, got none; last error = %v, output = %+v", lastErr, out)
	return mcpserver.DiagnosticsOutput{}
}

func waitForRustDiagnostics(t *testing.T) {
	t.Helper()

	if err := lsptest.SleepOrCancel(t.Context(), rustDiagnosticsLookup.RetryDelay); err != nil {
		t.Fatalf("context canceled while waiting for %s diagnostics: %v", rustDiagnosticsLookup.ServerName, err)
	}
}
