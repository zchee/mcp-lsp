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

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/zchee/mcp-lsp/pkg/lsp"
	"github.com/zchee/mcp-lsp/tests/internal/lsptest"
)

var goReferencesLookup = lsptest.LookupConfig{
	Language:   "go",
	ServerName: "gopls",
	Attempts:   10,
	RetryDelay: 250 * time.Millisecond,
}

func TestIntegrationFindReferencesWithGopls(t *testing.T) {
	requireIntegration(t)

	ws := extractFixture(t, "references_suite.txtar")
	mgr := newManager(t, ws)

	greetFile := ws.Path("greet.go")
	greetText := ws.Source(t, "greet.go")
	greetURI := uri.File(greetFile).String()
	mainURI := uri.File(ws.Path("main.go")).String()
	declPos := ws.MarkerPosition(t, "greet.go", "decl", "Greeting")
	usePos := ws.MarkerPosition(t, "main.go", "use", "Greeting")
	use2Pos := ws.MarkerPosition(t, "main.go", "use2", "Greeting")

	withDecl := lookupGoReferences(t, mgr, greetFile, greetText, declPos, true, 3)
	assertReferenceAt(t, withDecl, greetURI, declPos)
	assertReferenceAt(t, withDecl, mainURI, usePos)
	assertReferenceAt(t, withDecl, mainURI, use2Pos)

	withoutDecl := lookupGoReferences(t, mgr, greetFile, greetText, declPos, false, 2)
	assertReferenceAt(t, withoutDecl, mainURI, usePos)
	assertReferenceAt(t, withoutDecl, mainURI, use2Pos)
	assertNoReferenceAt(t, withoutDecl, greetURI, declPos)
}

// lookupGoReferences drives lsp.References.Lookup against real gopls, retrying
// while the server is still indexing the workspace. gopls returns partial or
// empty reference sets before indexing settles, so success requires at least
// minCount locations.
func lookupGoReferences(t *testing.T, mgr *lsp.Manager, absPath, text string, pos protocol.Position, includeDeclaration bool, minCount int) []lsp.NavigationLocation {
	t.Helper()

	var (
		refs    []lsp.NavigationLocation
		lastErr error
	)
	for range goReferencesLookup.Attempts {
		refs, lastErr = mgr.References().Lookup(t.Context(), goReferencesLookup.Language, absPath, text, pos, includeDeclaration)
		if lastErr == nil && len(refs) >= minCount {
			return refs
		}
		if ctxErr := lsptest.SleepOrCancel(t.Context(), goReferencesLookup.RetryDelay); ctxErr != nil {
			t.Fatalf("context canceled while waiting for %s: %v", goReferencesLookup.ServerName, ctxErr)
		}
	}
	t.Fatalf("references did not reach %d locations after %d attempts; last error = %v, refs = %+v", minCount, goReferencesLookup.Attempts, lastErr, refs)
	return nil
}

func assertReferenceAt(t *testing.T, refs []lsp.NavigationLocation, wantURI string, want protocol.Position) {
	t.Helper()

	if !hasReferenceAt(refs, wantURI, want) {
		t.Fatalf("no reference pointed to %s at %d:%d (zero-based); refs = %+v", wantURI, want.Line, want.Character, refs)
	}
}

func assertNoReferenceAt(t *testing.T, refs []lsp.NavigationLocation, wantURI string, want protocol.Position) {
	t.Helper()

	if hasReferenceAt(refs, wantURI, want) {
		t.Fatalf("reference set includes the declaration at %s %d:%d although includeDeclaration=false; refs = %+v", wantURI, want.Line, want.Character, refs)
	}
}

func hasReferenceAt(refs []lsp.NavigationLocation, wantURI string, want protocol.Position) bool {
	for _, ref := range refs {
		if ref.TargetURI != wantURI {
			continue
		}
		if int64(ref.TargetRange.StartLine) == int64(want.Line) && int64(ref.TargetRange.StartColumn) == int64(want.Character) {
			return true
		}
	}
	return false
}
