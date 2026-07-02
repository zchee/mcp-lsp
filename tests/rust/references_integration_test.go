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
	"time"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/zchee/mcp-lsp/pkg/lsp"
	"github.com/zchee/mcp-lsp/tests/internal/lsptest"
)

var rustReferencesLookup = lsptest.LookupConfig{
	Language:   rustLanguage,
	ServerName: rustAnalyzerCommand,
	Attempts:   20,
	RetryDelay: 250 * time.Millisecond,
}

func TestIntegrationRustAnalyzerFindReferences(t *testing.T) {
	requireIntegration(t)

	ws := extractFixture(t, "references_suite.txtar")
	mgr := newManager(t, ws)

	libFile := ws.Path("src/lib.rs")
	libText := ws.Source(t, "src/lib.rs")
	libURI := uri.File(libFile).String()
	declPos := ws.MarkerPosition(t, "src/lib.rs", "decl", "greeting")
	usePos := ws.MarkerPosition(t, "src/lib.rs", "use", "greeting")
	use2Pos := ws.MarkerPosition(t, "src/lib.rs", "use2", "greeting")

	withDecl := lookupRustReferences(t, mgr, libFile, libText, declPos, true, 3)
	assertRustReferenceAt(t, withDecl, libURI, declPos)
	assertRustReferenceAt(t, withDecl, libURI, usePos)
	assertRustReferenceAt(t, withDecl, libURI, use2Pos)

	withoutDecl := lookupRustReferences(t, mgr, libFile, libText, declPos, false, 2)
	assertRustReferenceAt(t, withoutDecl, libURI, usePos)
	assertRustReferenceAt(t, withoutDecl, libURI, use2Pos)
	if hasRustReferenceAt(withoutDecl, libURI, declPos) {
		t.Fatalf("reference set includes the declaration although includeDeclaration=false; refs = %+v", withoutDecl)
	}
}

// lookupRustReferences drives lsp.References.Lookup against real
// rust-analyzer, retrying while the server is still indexing. rust-analyzer
// returns empty or partial reference sets before indexing settles, so success
// requires at least minCount locations.
func lookupRustReferences(t *testing.T, mgr *lsp.Manager, absPath, text string, pos protocol.Position, includeDeclaration bool, minCount int) []lsp.NavigationLocation {
	t.Helper()

	var (
		refs    []lsp.NavigationLocation
		lastErr error
	)
	for range rustReferencesLookup.Attempts {
		refs, lastErr = mgr.References().Lookup(t.Context(), rustReferencesLookup.Language, absPath, text, pos, includeDeclaration)
		if lastErr == nil && len(refs) >= minCount {
			return refs
		}
		if ctxErr := lsptest.SleepOrCancel(t.Context(), rustReferencesLookup.RetryDelay); ctxErr != nil {
			t.Fatalf("context canceled while waiting for %s: %v", rustReferencesLookup.ServerName, ctxErr)
		}
	}
	t.Fatalf("references did not reach %d locations after %d attempts; last error = %v, refs = %+v", minCount, rustReferencesLookup.Attempts, lastErr, refs)
	return nil
}

func assertRustReferenceAt(t *testing.T, refs []lsp.NavigationLocation, wantURI string, want protocol.Position) {
	t.Helper()

	if !hasRustReferenceAt(refs, wantURI, want) {
		t.Fatalf("no reference pointed to %s at %d:%d (zero-based); refs = %+v", wantURI, want.Line, want.Character, refs)
	}
}

func hasRustReferenceAt(refs []lsp.NavigationLocation, wantURI string, want protocol.Position) bool {
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
