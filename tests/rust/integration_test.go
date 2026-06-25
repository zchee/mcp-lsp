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
	"testing"

	"go.lsp.dev/uri"

	"github.com/zchee/mcp-lsp/pkg/lsp"
	"github.com/zchee/mcp-lsp/tests/internal/lsptest"
)

func TestIntegrationRustAnalyzerDiagnosticsReportsCompileError(t *testing.T) {
	requireIntegration(t)

	ws := extractFixture(t, "diagnostics_error.txtar")
	mgr := newManager(t, ws)

	mainFile := ws.Path("src/main.rs")
	text := ws.Source(t, "src/main.rs")

	diags := lookupRustDiagnostics(t, mgr, mainFile, text)

	var foundError bool
	for _, d := range diags {
		if d.Severity != "error" {
			continue
		}
		foundError = true
		// Domain positions are zero-based; rust-analyzer must report a
		// non-negative range for the syntax error.
		if d.StartLine < 0 || d.StartColumn < 0 {
			t.Errorf("diagnostic positions must be non-negative: %+v", d)
		}
	}
	if !foundError {
		t.Errorf("no error-severity diagnostic reported; diagnostics = %+v", diags)
	}
}

func TestIntegrationRustAnalyzerDefinitionResolvesAcrossFiles(t *testing.T) {
	requireIntegration(t)

	ws := extractFixture(t, "definition_crossfile.txtar")
	mgr := newManager(t, ws)

	mainFile := ws.Path("src/main.rs")
	text := ws.Source(t, "src/main.rs")
	query := ws.MarkerPosition(t, "src/main.rs", "query", "Greeting")
	target := ws.MarkerPosition(t, "src/lib.rs", "target", "Greeting")

	defs := lsptest.LookupDefinition(t, mgr, rustDefinitionLookup, mainFile, text, query)
	lsptest.AssertDefinitionResolvesTo(t, defs, string(uri.File(ws.Path("src/lib.rs"))), target)
}

func TestIntegrationRustAnalyzerImplementationResolvesTraitMethod(t *testing.T) {
	requireIntegration(t)

	ws := extractFixture(t, "implementation_trait.txtar")
	mgr := newManager(t, ws)

	mainFile := ws.Path("src/main.rs")
	text := ws.Source(t, "src/main.rs")
	query := ws.MarkerPosition(t, "src/main.rs", "query", "greet")
	target := ws.MarkerPosition(t, "src/main.rs", "target", "greet")

	implementations := lsptest.LookupImplementation(t, mgr, rustImplementationLookup, mainFile, text, query)
	lsptest.AssertImplementationResolvesTo(t, implementations, string(uri.File(mainFile)), target)
}

func lookupRustDiagnostics(t *testing.T, mgr *lsp.Manager, mainFile, text string) []lsp.Diagnostic {
	t.Helper()

	var (
		diags   []lsp.Diagnostic
		lastErr error
	)
	for range rustDiagnosticsLookup.Attempts {
		diags, lastErr = mgr.Diagnostics().Lookup(t.Context(), rustLanguage, mainFile, text)
		if lastErr == nil && len(diags) > 0 {
			return diags
		}
		if err := lsptest.SleepOrCancel(t.Context(), rustDiagnosticsLookup.RetryDelay); err != nil {
			t.Fatalf("context canceled while waiting for %s diagnostics: %v", rustDiagnosticsLookup.ServerName, err)
		}
	}
	t.Fatalf("expected at least one diagnostic for a Rust file with a compile error, got none; last error = %v", lastErr)
	return nil
}
