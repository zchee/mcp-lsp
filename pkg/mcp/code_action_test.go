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

package mcp

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

type fakeCodeActionLooker struct {
	actions    []lsp.CodeAction
	gotLang    string
	gotPath    string
	gotText    string
	gotRange   protocol.Range
	gotOnly    []protocol.CodeActionKind
	gotResolve bool
	calls      int
}

func (f *fakeCodeActionLooker) Lookup(_ context.Context, lang, absPath, text string, rng protocol.Range, only []protocol.CodeActionKind, resolve bool) ([]lsp.CodeAction, error) {
	f.calls++
	f.gotLang = lang
	f.gotPath = absPath
	f.gotText = text
	f.gotRange = rng
	f.gotOnly = append([]protocol.CodeActionKind(nil), only...)
	f.gotResolve = resolve
	return f.actions, nil
}

func TestCodeActionHandlerValidatesRangeBeforeFileIO(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "missing.go")
	looker := &fakeCodeActionLooker{}
	handler := codeActionHandler(looker, t.TempDir(), testResolver(t, "go", "python", "rust"))

	_, _, err := handler(t.Context(), nil, CodeActionInput{
		File:        missing,
		StartLine:   1,
		StartColumn: 0,
		EndLine:     1,
		EndColumn:   1,
	})
	if err == nil || !strings.Contains(err.Error(), "column must be greater than zero") {
		t.Fatalf("code action error = %v, want column validation error", err)
	}
	if looker.calls != 0 {
		t.Fatalf("CodeAction calls = %d, want 0", looker.calls)
	}
}

func TestCodeActionHandlerConvertsRangeKindsAndEdits(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	fileURI := uri.File(path)
	isPreferred := true
	edit := lsp.WorkspaceEdit{Changes: map[string][]lsp.WorkspaceTextEdit{
		fileURI.String(): {{Range: lsp.NavigationRange{StartLine: 0, StartColumn: 0, EndLine: 0, EndColumn: 4}, NewText: "fixed"}},
	}}
	looker := &fakeCodeActionLooker{
		actions: []lsp.CodeAction{
			{
				Title:       "Fix",
				Kind:        string(protocol.CodeActionKindQuickFix),
				IsPreferred: &isPreferred,
				Edit:        &edit,
				Command:     &lsp.Command{Title: "Apply", Tooltip: "apply edit", Command: "server.apply"},
			},
		},
	}
	handler := codeActionHandler(looker, t.TempDir(), testResolver(t, "go", "python", "rust"))

	_, out, err := handler(t.Context(), nil, CodeActionInput{
		File:        path,
		StartLine:   1,
		StartColumn: 1,
		EndLine:     1,
		EndColumn:   5,
		Only:        []string{string(protocol.CodeActionKindQuickFix)},
		Resolve:     true,
	})
	if err != nil {
		t.Fatalf("code action handler: %v", err)
	}
	wantRange := protocol.Range{Start: protocol.Position{Line: 0, Character: 0}, End: protocol.Position{Line: 0, Character: 4}}
	if looker.gotRange != wantRange {
		t.Fatalf("Lookup range = %+v, want %+v", looker.gotRange, wantRange)
	}
	if diff := gocmp.Diff([]protocol.CodeActionKind{protocol.CodeActionKindQuickFix}, looker.gotOnly); diff != "" {
		t.Fatalf("Lookup only mismatch (-want +got):\n%s", diff)
	}
	if !looker.gotResolve {
		t.Fatal("Lookup resolve = false, want true")
	}
	wantEdit := protocol.WorkspaceEdit{Changes: map[uri.URI][]protocol.TextEdit{
		fileURI: {{Range: wantRange, NewText: "fixed"}},
	}}
	want := CodeActionOutput{
		File: path,
		URI:  fileURI.String(),
		Actions: []CodeActionItem{
			{
				Title:       "Fix",
				Kind:        string(protocol.CodeActionKindQuickFix),
				IsPreferred: &isPreferred,
				Edit:        &wantEdit,
				Command:     &CommandItem{Title: "Apply", Tooltip: "apply edit", Command: "server.apply"},
			},
		},
	}
	if diff := gocmp.Diff(want, out); diff != "" {
		t.Fatalf("code action output mismatch (-want +got):\n%s", diff)
	}
}
