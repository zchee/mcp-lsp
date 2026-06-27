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

package pythonintegration

import (
	"fmt"
	"strings"
	"testing"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/zchee/mcp-lsp/pkg/lsp"
	"github.com/zchee/mcp-lsp/tests/internal/lsptest"
)

func TestIntegrationPyrightFeatureSuitePreviews(t *testing.T) {
	requireIntegration(t)

	ws := extractFixture(t, "feature_suite.txtar")
	mgr := newManager(t, ws)

	mainFile := ws.Path("main.py")
	mainURI := uri.File(mainFile).String()
	text := ws.Source(t, "main.py")
	hoverPos := ws.MarkerPosition(t, "main.py", "hover", "ANSWER")
	renamePos := ws.MarkerPosition(t, "main.py", "rename", "message")
	queryPos := ws.MarkerPosition(t, "main.py", "query", "message")

	hover := lookupPyrightHover(t, mgr, mainFile, text, hoverPos)
	if hover == nil {
		t.Fatal("hover result is nil")
	}
	if hover.Value == "" || !strings.Contains(hover.Value, "ANSWER") {
		t.Fatalf("hover value = %q, want documentation for ANSWER", hover.Value)
	}
	if hover.Range != nil && (hover.Range.StartLine < 0 || hover.Range.StartColumn < 0) {
		t.Fatalf("hover range must use non-negative zero-based positions: %+v", hover.Range)
	}

	symbols := lookupPyrightWorkspaceSymbols(t, mgr, "message")
	assertPyrightWorkspaceSymbol(t, symbols, "message", mainURI)

	assertPyrightFormattingPreviewOrUnsupported(t, mgr, mainFile, text, mainURI)
	assertPyrightRangeFormattingPreviewOrUnsupported(t, mgr, mainFile, text, mainURI, protocol.Range{
		Start: protocol.Position{Line: 2, Character: 0},
		End:   protocol.Position{Line: 5, Character: 24},
	})

	renameEdit := previewPyrightRename(t, mgr, mainFile, text, queryPos, "greeting_message")
	edits := assertPyrightTextEditForURI(t, "rename", renameEdit, mainURI)
	assertPyrightRenameEdits(t, edits, renamePos, queryPos)
}

func lookupPyrightHover(t *testing.T, mgr *lsp.Manager, absPath, text string, pos protocol.Position) *lsp.HoverResult {
	t.Helper()
	validatePyrightFeatureLookupConfig(t)

	var (
		hover   *lsp.HoverResult
		lastErr error
	)
	for range pyrightFeatureLookup.Attempts {
		hover, lastErr = mgr.Hover().Lookup(t.Context(), pyrightFeatureLookup.Language, absPath, text, pos)
		if lastErr == nil && hover != nil && hover.Value != "" {
			return hover
		}
		waitForPyrightFeature(t)
	}
	t.Fatalf("no hover resolved after %d attempts; last error = %v, hover = %+v", pyrightFeatureLookup.Attempts, lastErr, hover)
	return nil
}

func lookupPyrightWorkspaceSymbols(t *testing.T, mgr *lsp.Manager, query string) []lsp.WorkspaceSymbol {
	t.Helper()
	validatePyrightFeatureLookupConfig(t)

	var (
		symbols []lsp.WorkspaceSymbol
		lastErr error
	)
	for range pyrightFeatureLookup.Attempts {
		symbols, lastErr = mgr.WorkspaceSymbols().Lookup(t.Context(), pyrightFeatureLookup.Language, query)
		if lastErr == nil && len(symbols) > 0 {
			return symbols
		}
		waitForPyrightFeature(t)
	}
	t.Fatalf("no workspace symbols resolved after %d attempts; last error = %v, symbols = %+v", pyrightFeatureLookup.Attempts, lastErr, symbols)
	return nil
}

func assertPyrightFormattingPreviewOrUnsupported(t *testing.T, mgr *lsp.Manager, absPath, text, wantURI string) {
	t.Helper()
	validatePyrightFeatureLookupConfig(t)

	options := protocol.FormattingOptions{TabSize: 4, InsertSpaces: true}
	edit, err := mgr.Formatting().Format(t.Context(), pyrightFeatureLookup.Language, absPath, text, options)
	if err != nil {
		assertPyrightUnsupportedError(t, "formatting", err, "formatting request is not supported by language server")
		return
	}
	assertPyrightTextEditForURI(t, "formatting", edit, wantURI)
}

func assertPyrightRangeFormattingPreviewOrUnsupported(t *testing.T, mgr *lsp.Manager, absPath, text, wantURI string, rng protocol.Range) {
	t.Helper()
	validatePyrightFeatureLookupConfig(t)

	options := protocol.FormattingOptions{TabSize: 4, InsertSpaces: true}
	edit, err := mgr.Formatting().RangeFormat(t.Context(), pyrightFeatureLookup.Language, absPath, text, rng, options)
	if err != nil {
		assertPyrightUnsupportedError(t, "range formatting", err, "range formatting request is not supported by language server")
		return
	}
	assertPyrightTextEditForURI(t, "range formatting", edit, wantURI)
}

func previewPyrightRename(t *testing.T, mgr *lsp.Manager, absPath, text string, pos protocol.Position, newName string) lsp.WorkspaceEdit {
	t.Helper()
	validatePyrightFeatureLookupConfig(t)

	var (
		edit    lsp.WorkspaceEdit
		lastErr error
	)
	for range pyrightFeatureLookup.Attempts {
		edit, lastErr = mgr.Rename().Preview(t.Context(), pyrightFeatureLookup.Language, absPath, text, pos, newName)
		if lastErr == nil && pyrightWorkspaceEditHasTextEdits(edit) {
			return edit
		}
		waitForPyrightFeature(t)
	}
	t.Fatalf("no rename edits after %d attempts; last error = %v, edit = %+v", pyrightFeatureLookup.Attempts, lastErr, edit)
	return lsp.WorkspaceEdit{}
}

func assertPyrightWorkspaceSymbol(t *testing.T, symbols []lsp.WorkspaceSymbol, wantName, wantURI string) {
	t.Helper()

	for _, symbol := range symbols {
		if symbol.Name != wantName || symbol.URI != wantURI {
			continue
		}
		if symbol.Range == nil {
			t.Fatalf("workspace symbol %q has nil range: %+v", wantName, symbol)
		}
		if symbol.Range.StartLine < 0 || symbol.Range.StartColumn < 0 {
			t.Fatalf("workspace symbol range must be zero-based and non-negative: %+v", symbol.Range)
		}
		return
	}
	t.Fatalf("no workspace symbol %q at %s; symbols = %+v", wantName, wantURI, symbols)
}

func assertPyrightTextEditForURI(t *testing.T, label string, edit lsp.WorkspaceEdit, wantURI string) []lsp.WorkspaceTextEdit {
	t.Helper()

	edits := pyrightTextEditsForURI(edit, wantURI)
	if len(edits) == 0 {
		t.Fatalf("%s returned no text edits for %s; edit = %+v", label, wantURI, edit)
	}
	for _, te := range edits {
		if te.Range.StartLine < 0 || te.Range.StartColumn < 0 || te.Range.EndLine < 0 || te.Range.EndColumn < 0 {
			t.Fatalf("%s edit has negative zero-based range: %+v", label, te)
		}
	}
	return edits
}

func pyrightTextEditsForURI(edit lsp.WorkspaceEdit, wantURI string) []lsp.WorkspaceTextEdit {
	out := append([]lsp.WorkspaceTextEdit(nil), edit.Changes[wantURI]...)
	for _, change := range edit.DocumentChanges {
		if change.TextDocumentEdit == nil || change.TextDocumentEdit.TextDocument.URI != wantURI {
			continue
		}
		out = append(out, change.TextDocumentEdit.Edits...)
	}
	return out
}

func assertPyrightRenameEdits(t *testing.T, edits []lsp.WorkspaceTextEdit, declaration, reference protocol.Position) {
	t.Helper()

	wantStarts := map[string]bool{
		editStartKey(declaration): false,
		editStartKey(reference):   false,
	}
	var matchingEdits []lsp.WorkspaceTextEdit
	for _, edit := range edits {
		if edit.NewText != "greeting_message" {
			continue
		}
		matchingEdits = append(matchingEdits, edit)
		key := fmt.Sprintf("%d:%d", edit.Range.StartLine, edit.Range.StartColumn)
		if _, ok := wantStarts[key]; ok {
			wantStarts[key] = true
		}
	}
	for key, found := range wantStarts {
		if !found {
			t.Fatalf("rename edit for %s not found; matching edits = %+v; all edits = %+v", key, matchingEdits, edits)
		}
	}
}

func editStartKey(pos protocol.Position) string {
	return fmt.Sprintf("%d:%d", pos.Line, pos.Character)
}

func pyrightWorkspaceEditHasTextEdits(edit lsp.WorkspaceEdit) bool {
	for _, edits := range edit.Changes {
		if len(edits) > 0 {
			return true
		}
	}
	for _, change := range edit.DocumentChanges {
		if change.TextDocumentEdit != nil && len(change.TextDocumentEdit.Edits) > 0 {
			return true
		}
	}
	return false
}

func assertPyrightUnsupportedError(t *testing.T, label string, err error, want string) {
	t.Helper()

	if err.Error() != want {
		t.Fatalf("%s failed with unexpected error: %v", label, err)
	}
}

func validatePyrightFeatureLookupConfig(t *testing.T) {
	t.Helper()

	if pyrightFeatureLookup.Language == "" || pyrightFeatureLookup.ServerName == "" || pyrightFeatureLookup.Attempts <= 0 || pyrightFeatureLookup.RetryDelay <= 0 {
		t.Fatalf("invalid pyright feature lookup config: %+v", pyrightFeatureLookup)
	}
}

func waitForPyrightFeature(t *testing.T) {
	t.Helper()

	if err := lsptest.SleepOrCancel(t.Context(), pyrightFeatureLookup.RetryDelay); err != nil {
		t.Fatalf("context canceled while waiting for %s feature result: %v", pyrightFeatureLookup.ServerName, err)
	}
}
