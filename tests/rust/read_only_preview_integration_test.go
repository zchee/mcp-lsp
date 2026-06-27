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
	"strings"
	"testing"
	"time"

	gocmp "github.com/google/go-cmp/cmp"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/zchee/mcp-lsp/pkg/lsp"
	"github.com/zchee/mcp-lsp/tests/internal/lsptest"
)

var rustReadOnlyPreviewLookup = lsptest.LookupConfig{
	Language:   rustLanguage,
	ServerName: rustAnalyzerCommand,
	Attempts:   20,
	RetryDelay: 250 * time.Millisecond,
}

func TestIntegrationRustAnalyzerReadOnlyPreviews(t *testing.T) {
	requireIntegration(t)

	ws := extractFixture(t, "read_only_preview_suite.txtar")
	mgr := newManager(t, ws)

	mainFile := ws.Path("src/main.rs")
	mainURI := uri.File(mainFile).String()
	text := ws.Source(t, "src/main.rs")
	hoverPos := ws.MarkerPosition(t, "src/main.rs", "hover", "ANSWER")
	renamePos := ws.MarkerPosition(t, "src/main.rs", "rename", "message")
	queryPos := ws.MarkerPosition(t, "src/main.rs", "query", "message")

	hover := lookupRustHover(t, mgr, mainFile, text, hoverPos)
	if hover == nil {
		t.Fatal("hover result is nil")
	}
	if hover.Value == "" || !strings.Contains(hover.Value, "ANSWER") {
		t.Fatalf("hover value = %q, want documentation for ANSWER", hover.Value)
	}
	if hover.Range != nil && (hover.Range.StartLine < 0 || hover.Range.StartColumn < 0) {
		t.Fatalf("hover range must use non-negative zero-based positions: %+v", hover.Range)
	}

	symbols := lookupRustWorkspaceSymbols(t, mgr, "message")
	lsptest.AssertWorkspaceSymbol(t, symbols, "message", mainURI)

	formatEdit := previewRustFormatting(t, mgr, mainFile, text)
	lsptest.AssertTextEditForURI(t, "formatting", formatEdit, mainURI)

	assertRustRangeFormattingPreviewOrUnsupported(t, mgr, mainFile, text, mainURI, protocol.Range{
		Start: protocol.Position{Line: 2, Character: 0},
		End:   protocol.Position{Line: 4, Character: 1},
	})

	renameEdit := previewRustRename(t, mgr, mainFile, text, queryPos, "greeting_message")
	edits := lsptest.AssertTextEditForURI(t, "rename", renameEdit, mainURI)
	assertRustRenameEdits(t, edits, renamePos, queryPos)
}

func lookupRustHover(t *testing.T, mgr *lsp.Manager, absPath, text string, pos protocol.Position) *lsp.HoverResult {
	t.Helper()
	validateRustReadOnlyPreviewLookupConfig(t)

	var (
		hover   *lsp.HoverResult
		lastErr error
	)
	for range rustReadOnlyPreviewLookup.Attempts {
		hover, lastErr = mgr.Hover().Lookup(t.Context(), rustReadOnlyPreviewLookup.Language, absPath, text, pos)
		if lastErr == nil && hover != nil && hover.Value != "" {
			return hover
		}
		waitForRustReadOnlyPreview(t)
	}
	t.Fatalf("no hover resolved after %d attempts; last error = %v, hover = %+v", rustReadOnlyPreviewLookup.Attempts, lastErr, hover)
	return nil
}

func lookupRustWorkspaceSymbols(t *testing.T, mgr *lsp.Manager, query string) []lsp.WorkspaceSymbol {
	t.Helper()
	validateRustReadOnlyPreviewLookupConfig(t)

	var (
		symbols []lsp.WorkspaceSymbol
		lastErr error
	)
	for range rustReadOnlyPreviewLookup.Attempts {
		symbols, lastErr = mgr.WorkspaceSymbols().Lookup(t.Context(), rustReadOnlyPreviewLookup.Language, query)
		if lastErr == nil && len(symbols) > 0 {
			return symbols
		}
		waitForRustReadOnlyPreview(t)
	}
	t.Fatalf("no workspace symbols resolved after %d attempts; last error = %v, symbols = %+v", rustReadOnlyPreviewLookup.Attempts, lastErr, symbols)
	return nil
}

func previewRustFormatting(t *testing.T, mgr *lsp.Manager, absPath, text string) lsp.WorkspaceEdit {
	t.Helper()
	validateRustReadOnlyPreviewLookupConfig(t)

	var (
		edit    lsp.WorkspaceEdit
		lastErr error
	)
	options := protocol.FormattingOptions{TabSize: 4, InsertSpaces: true}
	for range rustReadOnlyPreviewLookup.Attempts {
		edit, lastErr = mgr.Formatting().Format(t.Context(), rustReadOnlyPreviewLookup.Language, absPath, text, options)
		if lastErr == nil && lsptest.WorkspaceEditHasTextEdits(edit) {
			return edit
		}
		waitForRustReadOnlyPreview(t)
	}
	t.Fatalf("no formatting edits after %d attempts; last error = %v, edit = %+v", rustReadOnlyPreviewLookup.Attempts, lastErr, edit)
	return lsp.WorkspaceEdit{}
}

func assertRustRangeFormattingPreviewOrUnsupported(t *testing.T, mgr *lsp.Manager, absPath, text, wantURI string, rng protocol.Range) {
	t.Helper()
	validateRustReadOnlyPreviewLookupConfig(t)

	options := protocol.FormattingOptions{TabSize: 4, InsertSpaces: true}
	edit, err := mgr.Formatting().RangeFormat(t.Context(), rustReadOnlyPreviewLookup.Language, absPath, text, rng, options)
	if err != nil {
		if strings.Contains(err.Error(), "range formatting request is not supported") {
			return
		}
		t.Fatalf("range formatting failed with unexpected error: %v", err)
	}
	lsptest.AssertTextEditForURI(t, "range formatting", edit, wantURI)
}

func previewRustRename(t *testing.T, mgr *lsp.Manager, absPath, text string, pos protocol.Position, newName string) lsp.WorkspaceEdit {
	t.Helper()
	validateRustReadOnlyPreviewLookupConfig(t)

	var (
		edit    lsp.WorkspaceEdit
		lastErr error
	)
	for range rustReadOnlyPreviewLookup.Attempts {
		edit, lastErr = mgr.Rename().Preview(t.Context(), rustReadOnlyPreviewLookup.Language, absPath, text, pos, newName)
		if lastErr == nil && lsptest.WorkspaceEditHasTextEdits(edit) {
			return edit
		}
		waitForRustReadOnlyPreview(t)
	}
	t.Fatalf("no rename edits after %d attempts; last error = %v, edit = %+v", rustReadOnlyPreviewLookup.Attempts, lastErr, edit)
	return lsp.WorkspaceEdit{}
}

func assertRustRenameEdits(t *testing.T, edits []lsp.WorkspaceTextEdit, declaration, reference protocol.Position) {
	t.Helper()

	wantStarts := []lsp.NavigationRange{
		{StartLine: int(declaration.Line), StartColumn: int(declaration.Character)},
		{StartLine: int(reference.Line), StartColumn: int(reference.Character)},
	}
	var gotStarts []lsp.NavigationRange
	for _, edit := range edits {
		if edit.NewText != "greeting_message" {
			continue
		}
		gotStarts = append(gotStarts, lsp.NavigationRange{StartLine: edit.Range.StartLine, StartColumn: edit.Range.StartColumn})
	}
	if diff := gocmp.Diff(wantStarts, gotStarts); diff != "" {
		t.Fatalf("rename edit starts mismatch (-want +got):\n%s\nedits = %+v", diff, edits)
	}
}

func validateRustReadOnlyPreviewLookupConfig(t *testing.T) {
	t.Helper()

	if rustReadOnlyPreviewLookup.Language == "" || rustReadOnlyPreviewLookup.ServerName == "" || rustReadOnlyPreviewLookup.Attempts <= 0 || rustReadOnlyPreviewLookup.RetryDelay <= 0 {
		t.Fatalf("invalid Rust read-only preview lookup config: %+v", rustReadOnlyPreviewLookup)
	}
}

func waitForRustReadOnlyPreview(t *testing.T) {
	t.Helper()

	if err := lsptest.SleepOrCancel(t.Context(), rustReadOnlyPreviewLookup.RetryDelay); err != nil {
		t.Fatalf("context canceled while waiting for %s read-only preview result: %v", rustReadOnlyPreviewLookup.ServerName, err)
	}
}
