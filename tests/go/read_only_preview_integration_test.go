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
	"strings"
	"testing"
	"time"

	gocmp "github.com/google/go-cmp/cmp"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/zchee/mcp-lsp/pkg/lsp"
	"github.com/zchee/mcp-lsp/tests/internal/lsptest"
)

var goReadOnlyPreviewLookup = lsptest.LookupConfig{
	Language:   "go",
	ServerName: "gopls",
	Attempts:   10,
	RetryDelay: 250 * time.Millisecond,
}

func TestIntegrationReadOnlyPreviewsWithGopls(t *testing.T) {
	requireIntegration(t)

	ws := extractFixture(t, "read_only_preview_suite.txtar")
	mgr := newManager(t, ws)

	mainFile := ws.Path("main.go")
	mainURI := uri.File(mainFile).String()
	text := ws.Source(t, "main.go")
	hoverPos := ws.MarkerPosition(t, "main.go", "hover", "answer")
	renamePos := ws.MarkerPosition(t, "main.go", "rename", "message")
	queryPos := ws.MarkerPosition(t, "main.go", "query", "message")

	hover := lookupHover(t, mgr, mainFile, text, hoverPos)
	if hover == nil {
		t.Fatal("hover result is nil")
	}
	if hover.Value == "" || !strings.Contains(hover.Value, "answer") {
		t.Fatalf("hover value = %q, want documentation for answer", hover.Value)
	}
	if hover.Range != nil && (hover.Range.StartLine < 0 || hover.Range.StartColumn < 0) {
		t.Fatalf("hover range must use non-negative zero-based positions: %+v", hover.Range)
	}

	symbols := lookupWorkspaceSymbols(t, mgr, "message")
	lsptest.AssertWorkspaceSymbol(t, symbols, "message", mainURI)

	formatEdit := previewFormatting(t, mgr, mainFile, text)
	lsptest.AssertTextEditForURI(t, "formatting", formatEdit, mainURI)

	assertRangeFormattingPreviewOrUnsupported(t, mgr, mainFile, text, mainURI, protocol.Range{
		Start: protocol.Position{Line: 6, Character: 0},
		End:   protocol.Position{Line: 8, Character: 1},
	})

	renameEdit := previewRename(t, mgr, mainFile, text, renamePos, "greetingMessage")
	edits := lsptest.AssertTextEditForURI(t, "rename", renameEdit, mainURI)
	assertRenameEdits(t, edits, renamePos, queryPos)
}

func lookupHover(t *testing.T, mgr *lsp.Manager, absPath, text string, pos protocol.Position) *lsp.HoverResult {
	t.Helper()
	validateReadOnlyPreviewLookupConfig(t)

	var (
		hover   *lsp.HoverResult
		lastErr error
	)
	for range goReadOnlyPreviewLookup.Attempts {
		hover, lastErr = mgr.Hover().Lookup(t.Context(), goReadOnlyPreviewLookup.Language, absPath, text, pos)
		if lastErr == nil && hover != nil && hover.Value != "" {
			return hover
		}
		waitForReadOnlyPreview(t)
	}
	t.Fatalf("no hover resolved after %d attempts; last error = %v, hover = %+v", goReadOnlyPreviewLookup.Attempts, lastErr, hover)
	return nil
}

func lookupWorkspaceSymbols(t *testing.T, mgr *lsp.Manager, query string) []lsp.WorkspaceSymbol {
	t.Helper()
	validateReadOnlyPreviewLookupConfig(t)

	var (
		symbols []lsp.WorkspaceSymbol
		lastErr error
	)
	for range goReadOnlyPreviewLookup.Attempts {
		symbols, lastErr = mgr.WorkspaceSymbols().Lookup(t.Context(), goReadOnlyPreviewLookup.Language, query)
		if lastErr == nil && len(symbols) > 0 {
			return symbols
		}
		waitForReadOnlyPreview(t)
	}
	t.Fatalf("no workspace symbols resolved after %d attempts; last error = %v, symbols = %+v", goReadOnlyPreviewLookup.Attempts, lastErr, symbols)
	return nil
}

func previewFormatting(t *testing.T, mgr *lsp.Manager, absPath, text string) lsp.WorkspaceEdit {
	t.Helper()
	validateReadOnlyPreviewLookupConfig(t)

	var (
		edit    lsp.WorkspaceEdit
		lastErr error
	)
	options := protocol.FormattingOptions{TabSize: 4, InsertSpaces: true}
	for range goReadOnlyPreviewLookup.Attempts {
		edit, lastErr = mgr.Formatting().Format(t.Context(), goReadOnlyPreviewLookup.Language, absPath, text, options)
		if lastErr == nil && lsptest.WorkspaceEditHasTextEdits(edit) {
			return edit
		}
		waitForReadOnlyPreview(t)
	}
	t.Fatalf("no formatting edits after %d attempts; last error = %v, edit = %+v", goReadOnlyPreviewLookup.Attempts, lastErr, edit)
	return lsp.WorkspaceEdit{}
}

func assertRangeFormattingPreviewOrUnsupported(t *testing.T, mgr *lsp.Manager, absPath, text, wantURI string, rng protocol.Range) {
	t.Helper()
	validateReadOnlyPreviewLookupConfig(t)

	options := protocol.FormattingOptions{TabSize: 4, InsertSpaces: true}
	edit, err := mgr.Formatting().RangeFormat(t.Context(), goReadOnlyPreviewLookup.Language, absPath, text, rng, options)
	if err != nil {
		if strings.Contains(err.Error(), "range formatting request is not supported") {
			return
		}
		t.Fatalf("range formatting failed with unexpected error: %v", err)
	}
	lsptest.AssertTextEditForURI(t, "range formatting", edit, wantURI)
}

func previewRename(t *testing.T, mgr *lsp.Manager, absPath, text string, pos protocol.Position, newName string) lsp.WorkspaceEdit {
	t.Helper()
	validateReadOnlyPreviewLookupConfig(t)

	var (
		edit    lsp.WorkspaceEdit
		lastErr error
	)
	for range goReadOnlyPreviewLookup.Attempts {
		edit, lastErr = mgr.Rename().Preview(t.Context(), goReadOnlyPreviewLookup.Language, absPath, text, pos, newName)
		if lastErr == nil && lsptest.WorkspaceEditHasTextEdits(edit) {
			return edit
		}
		waitForReadOnlyPreview(t)
	}
	t.Fatalf("no rename edits after %d attempts; last error = %v, edit = %+v", goReadOnlyPreviewLookup.Attempts, lastErr, edit)
	return lsp.WorkspaceEdit{}
}

func assertRenameEdits(t *testing.T, edits []lsp.WorkspaceTextEdit, declaration, reference protocol.Position) {
	t.Helper()

	wantStarts := []lsp.NavigationRange{
		{StartLine: int(declaration.Line), StartColumn: int(declaration.Character)},
		{StartLine: int(reference.Line), StartColumn: int(reference.Character)},
	}
	var gotStarts []lsp.NavigationRange
	for _, edit := range edits {
		if edit.NewText != "greetingMessage" {
			continue
		}
		gotStarts = append(gotStarts, lsp.NavigationRange{StartLine: edit.Range.StartLine, StartColumn: edit.Range.StartColumn})
	}
	if diff := gocmp.Diff(wantStarts, gotStarts); diff != "" {
		t.Fatalf("rename edit starts mismatch (-want +got):\n%s\nedits = %+v", diff, edits)
	}
}

func validateReadOnlyPreviewLookupConfig(t *testing.T) {
	t.Helper()

	if goReadOnlyPreviewLookup.Language == "" || goReadOnlyPreviewLookup.ServerName == "" || goReadOnlyPreviewLookup.Attempts <= 0 || goReadOnlyPreviewLookup.RetryDelay <= 0 {
		t.Fatalf("invalid read-only preview lookup config: %+v", goReadOnlyPreviewLookup)
	}
}

func waitForReadOnlyPreview(t *testing.T) {
	t.Helper()

	if err := lsptest.SleepOrCancel(t.Context(), goReadOnlyPreviewLookup.RetryDelay); err != nil {
		t.Fatalf("context canceled while waiting for %s read-only preview result: %v", goReadOnlyPreviewLookup.ServerName, err)
	}
}
