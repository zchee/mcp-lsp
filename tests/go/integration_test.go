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
	"testing"
	"time"

	"go.lsp.dev/uri"

	"github.com/zchee/mcp-lsp/tests/internal/lsptest"
)

var goDefinitionLookup = lsptest.DefinitionLookupConfig{
	Language:   "go",
	ServerName: "gopls",
	Attempts:   10,
	RetryDelay: 250 * time.Millisecond,
}

var goImplementationLookup = lsptest.ImplementationLookupConfig{
	Language:   "go",
	ServerName: "gopls",
	Attempts:   10,
	RetryDelay: 250 * time.Millisecond,
}

func TestIntegrationDiagnosticsReportsCompileError(t *testing.T) {
	requireIntegration(t)

	ws := extractFixture(t, "diagnostics_error.txtar")
	mgr := newManager(t, ws)

	mainFile := ws.Path("main.go")
	text := ws.Source(t, "main.go")

	diags, err := mgr.Diagnostics().Lookup(t.Context(), "go", mainFile, text)
	if err != nil {
		t.Fatalf("diagnostics lookup: %v", err)
	}
	if len(diags) == 0 {
		t.Fatal("expected at least one diagnostic for a file with a compile error, got none")
	}

	var foundError bool
	for _, d := range diags {
		if d.Severity != "error" {
			continue
		}
		foundError = true
		// Domain positions are zero-based; an error on the call must be past the
		// first line and non-negative.
		if d.StartLine < 0 || d.StartColumn < 0 {
			t.Errorf("diagnostic positions must be non-negative: %+v", d)
		}
	}
	if !foundError {
		t.Errorf("no error-severity diagnostic reported; diagnostics = %+v", diags)
	}
}

func TestIntegrationDefinitionResolvesLocalSymbol(t *testing.T) {
	requireIntegration(t)

	ws := extractFixture(t, "definition_local.txtar")
	mgr := newManager(t, ws)

	mainFile := ws.Path("main.go")
	text := ws.Source(t, "main.go")
	query := ws.MarkerPosition(t, "main.go", "query", "answer")
	target := ws.MarkerPosition(t, "main.go", "target", "answer")
	wantURI := string(uri.File(mainFile))

	defs := lsptest.LookupDefinition(t, mgr, goDefinitionLookup, mainFile, text, query)
	lsptest.AssertDefinitionResolvesTo(t, defs, wantURI, target)
}

func TestIntegrationDefinitionResolvesAcrossFiles(t *testing.T) {
	requireIntegration(t)

	ws := extractFixture(t, "definition_crossfile.txtar")
	mgr := newManager(t, ws)

	mainFile := ws.Path("main.go")
	text := ws.Source(t, "main.go")
	query := ws.MarkerPosition(t, "main.go", "query", "Greeting")
	target := ws.MarkerPosition(t, "lib.go", "target", "Greeting")
	wantURI := string(uri.File(ws.Path("lib.go")))

	defs := lsptest.LookupDefinition(t, mgr, goDefinitionLookup, mainFile, text, query)
	lsptest.AssertDefinitionResolvesTo(t, defs, wantURI, target)
}

func TestIntegrationImplementationResolvesInterfaceMethod(t *testing.T) {
	requireIntegration(t)

	ws := extractFixture(t, "implementation_interface.txtar")
	mgr := newManager(t, ws)

	mainFile := ws.Path("main.go")
	text := ws.Source(t, "main.go")
	query := ws.MarkerPosition(t, "main.go", "query", "Greet")
	target := ws.MarkerPosition(t, "main.go", "target", "Greet")
	wantURI := string(uri.File(mainFile))

	implementations := lsptest.LookupImplementation(t, mgr, goImplementationLookup, mainFile, text, query)
	lsptest.AssertImplementationResolvesTo(t, implementations, wantURI, target)
}
