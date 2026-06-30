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

func TestIntegrationPyrightReadOnlyPreviews(t *testing.T) {
	requireIntegration(t)

	ws := extractFixture(t, "read_only_preview_suite.txtar")
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
	lsptest.AssertWorkspaceSymbol(t, symbols, "message", mainURI)

	assertPyrightFormattingPreviewOrUnsupported(t, mgr, mainFile, text, mainURI)
	assertPyrightRangeFormattingPreviewOrUnsupported(t, mgr, mainFile, text, mainURI, protocol.Range{
		Start: protocol.Position{Line: 2, Character: 0},
		End:   protocol.Position{Line: 5, Character: 24},
	})

	renameEdit := previewPyrightRename(t, mgr, mainFile, text, queryPos, "greeting_message")
	edits := lsptest.AssertTextEditForURI(t, "rename", renameEdit, mainURI)
	assertPyrightRenameEdits(t, edits, renamePos, queryPos)
}

func TestIntegrationBasedPyrightLanguageAliases(t *testing.T) {
	lsptest.RequireIntegration(t, basedpyrightCommand)

	ws := extractFixture(t, "read_only_preview_suite.txtar")

	mainFile := ws.Path("main.py")
	text := ws.Source(t, "main.py")
	hoverPos := ws.MarkerPosition(t, "main.py", "hover", "ANSWER")

	tests := map[string]struct {
		language string
	}{
		"python canonical language": {
			language: "python",
		},
		"py alias": {
			language: "py",
		},
		"basedpyright alias": {
			language: "basedpyright",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mgr := newBasedPyrightManager(t, ws)
			diags, err := mgr.Diagnostics().Lookup(t.Context(), tt.language, mainFile, text)
			if err != nil {
				t.Fatalf("diagnostics with language %q: %v", tt.language, err)
			}
			if diags == nil {
				t.Fatalf("diagnostics with language %q returned nil slice", tt.language)
			}
		})
	}

	mgr := newBasedPyrightManager(t, ws)
	hover := lookupPythonHoverWithLanguage(t, mgr, "basedpyright", mainFile, text, hoverPos)
	if hover == nil || !strings.Contains(hover.Value, "ANSWER") {
		t.Fatalf("basedpyright hover = %+v, want documentation for ANSWER", hover)
	}

	_, err := mgr.Formatting().Format(t.Context(), "basedpyright", mainFile, text, protocol.FormattingOptions{TabSize: 4, InsertSpaces: true})
	if err != nil && strings.Contains(err.Error(), "illegal character") {
		t.Fatalf("basedpyright formatting routed through non-Python semantics: %v", err)
	}
}

func newBasedPyrightManager(t *testing.T, ws lsptest.Workspace) *lsp.Manager {
	t.Helper()

	cfg := map[string]lsp.ServerConfig{
		pythonLanguage: {
			Command:    basedpyrightCommand,
			Args:       []string{"--stdio"},
			LanguageID: protocol.LanguageKindPython,
		},
	}
	return lsptest.NewManager(t, cfg, ws)
}

func lookupPyrightHover(t *testing.T, mgr *lsp.Manager, absPath, text string, pos protocol.Position) *lsp.HoverResult {
	t.Helper()
	validatePyrightReadOnlyPreviewLookupConfig(t)

	return lookupPythonHoverWithLanguage(t, mgr, pyrightReadOnlyPreviewLookup.Language, absPath, text, pos)
}

func lookupPythonHoverWithLanguage(t *testing.T, mgr *lsp.Manager, language, absPath, text string, pos protocol.Position) *lsp.HoverResult {
	t.Helper()
	validatePyrightReadOnlyPreviewLookupConfig(t)

	var (
		hover   *lsp.HoverResult
		lastErr error
	)
	for range pyrightReadOnlyPreviewLookup.Attempts {
		hover, lastErr = mgr.Hover().Lookup(t.Context(), language, absPath, text, pos)
		if lastErr == nil && hover != nil && hover.Value != "" {
			return hover
		}
		waitForPyrightReadOnlyPreview(t)
	}
	t.Fatalf("no hover resolved after %d attempts; last error = %v, hover = %+v", pyrightReadOnlyPreviewLookup.Attempts, lastErr, hover)
	return nil
}

func lookupPyrightWorkspaceSymbols(t *testing.T, mgr *lsp.Manager, query string) []lsp.WorkspaceSymbol {
	t.Helper()
	validatePyrightReadOnlyPreviewLookupConfig(t)

	var (
		symbols []lsp.WorkspaceSymbol
		lastErr error
	)
	for range pyrightReadOnlyPreviewLookup.Attempts {
		symbols, lastErr = mgr.WorkspaceSymbols().Lookup(t.Context(), pyrightReadOnlyPreviewLookup.Language, query)
		if lastErr == nil && len(symbols) > 0 {
			return symbols
		}
		waitForPyrightReadOnlyPreview(t)
	}
	t.Fatalf("no workspace symbols resolved after %d attempts; last error = %v, symbols = %+v", pyrightReadOnlyPreviewLookup.Attempts, lastErr, symbols)
	return nil
}

func assertPyrightFormattingPreviewOrUnsupported(t *testing.T, mgr *lsp.Manager, absPath, text, wantURI string) {
	t.Helper()
	validatePyrightReadOnlyPreviewLookupConfig(t)

	options := protocol.FormattingOptions{TabSize: 4, InsertSpaces: true}
	edit, err := mgr.Formatting().Format(t.Context(), pyrightReadOnlyPreviewLookup.Language, absPath, text, options)
	if err != nil {
		assertPyrightUnsupportedError(t, "formatting", err, "formatting request is not supported by language server")
		return
	}
	lsptest.AssertTextEditForURI(t, "formatting", edit, wantURI)
}

func assertPyrightRangeFormattingPreviewOrUnsupported(t *testing.T, mgr *lsp.Manager, absPath, text, wantURI string, rng protocol.Range) {
	t.Helper()
	validatePyrightReadOnlyPreviewLookupConfig(t)

	options := protocol.FormattingOptions{TabSize: 4, InsertSpaces: true}
	edit, err := mgr.Formatting().RangeFormat(t.Context(), pyrightReadOnlyPreviewLookup.Language, absPath, text, rng, options)
	if err != nil {
		assertPyrightUnsupportedError(t, "range formatting", err, "range formatting request is not supported by language server")
		return
	}
	lsptest.AssertTextEditForURI(t, "range formatting", edit, wantURI)
}

func previewPyrightRename(t *testing.T, mgr *lsp.Manager, absPath, text string, pos protocol.Position, newName string) lsp.WorkspaceEdit {
	t.Helper()
	validatePyrightReadOnlyPreviewLookupConfig(t)

	var (
		edit    lsp.WorkspaceEdit
		lastErr error
	)
	for range pyrightReadOnlyPreviewLookup.Attempts {
		edit, lastErr = mgr.Rename().Preview(t.Context(), pyrightReadOnlyPreviewLookup.Language, absPath, text, pos, newName)
		if lastErr == nil && lsptest.WorkspaceEditHasTextEdits(edit) {
			return edit
		}
		waitForPyrightReadOnlyPreview(t)
	}
	t.Fatalf("no rename edits after %d attempts; last error = %v, edit = %+v", pyrightReadOnlyPreviewLookup.Attempts, lastErr, edit)
	return lsp.WorkspaceEdit{}
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

func assertPyrightUnsupportedError(t *testing.T, label string, err error, want string) {
	t.Helper()

	if err.Error() != want {
		t.Fatalf("%s failed with unexpected error: %v", label, err)
	}
}

func validatePyrightReadOnlyPreviewLookupConfig(t *testing.T) {
	t.Helper()

	if pyrightReadOnlyPreviewLookup.Language == "" || pyrightReadOnlyPreviewLookup.ServerName == "" || pyrightReadOnlyPreviewLookup.Attempts <= 0 || pyrightReadOnlyPreviewLookup.RetryDelay <= 0 {
		t.Fatalf("invalid pyright read-only preview lookup config: %+v", pyrightReadOnlyPreviewLookup)
	}
}

func waitForPyrightReadOnlyPreview(t *testing.T) {
	t.Helper()

	if err := lsptest.SleepOrCancel(t.Context(), pyrightReadOnlyPreviewLookup.RetryDelay); err != nil {
		t.Fatalf("context canceled while waiting for %s read-only preview result: %v", pyrightReadOnlyPreviewLookup.ServerName, err)
	}
}
