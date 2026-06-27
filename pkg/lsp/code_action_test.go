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

package lsp

import (
	"path/filepath"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestCodeActionsRejectUnsupportedCapabilityBeforeSync(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	srv := &fakeManagerServer{}
	mgr := newFeatureManager(t, srv, root)
	rng := protocol.Range{Start: protocol.Position{Line: 0, Character: 0}, End: protocol.Position{Line: 0, Character: 1}}

	_, err := mgr.CodeActions().Lookup(t.Context(), "go", path, "package main\n", rng, nil, false)
	requireErrorContains(t, err, "code action request is not supported")
	requireNoFeatureSync(t, srv)
}

func TestCodeActionsResolveWhenSupported(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	fileURI := uri.File(path)
	actionKind := protocol.CodeActionKindQuickFix
	actionRange := protocol.Range{Start: protocol.Position{Line: 0, Character: 0}, End: protocol.Position{Line: 0, Character: 4}}
	srv := &fakeManagerServer{
		capabilities: protocol.ServerCapabilities{
			CodeActionProvider: &protocol.CodeActionOptions{ResolveProvider: new(true)},
		},
		codeActions: []protocol.CommandOrCodeAction{
			&protocol.Command{Title: "Run", Tooltip: new("run server command"), Command: "server.run"},
			&protocol.CodeAction{
				Title:       "Fix",
				Kind:        new(protocol.CodeActionKindQuickFix),
				IsPreferred: new(true),
				Data:        protocol.LSPAny(`{"id":1}`),
			},
		},
		codeActionResolveResult: &protocol.CodeAction{
			Title:       "Fix",
			Kind:        new(protocol.CodeActionKindQuickFix),
			IsPreferred: new(true),
			Edit: &protocol.WorkspaceEdit{
				Changes: map[uri.URI][]protocol.TextEdit{
					fileURI: {{Range: actionRange, NewText: "fixed"}},
				},
			},
			Command: protocol.Command{Title: "Apply", Command: "server.apply"},
		},
	}
	mgr := newFeatureManager(t, srv, root)

	gotActions, err := mgr.CodeActions().Lookup(t.Context(), "go", path, "package main\n", actionRange, []protocol.CodeActionKind{actionKind}, true)
	if err != nil {
		t.Fatalf("CodeActions.Lookup: %v", err)
	}
	wantActions := []CodeAction{
		{Title: "Run", Command: &Command{Title: "Run", Tooltip: "run server command", Command: "server.run"}},
		{
			Title:       "Fix",
			Kind:        string(actionKind),
			IsPreferred: new(true),
			Edit: &WorkspaceEdit{Changes: map[string][]WorkspaceTextEdit{
				fileURI.String(): {{Range: NavigationRange{StartLine: 0, StartColumn: 0, EndLine: 0, EndColumn: 4}, NewText: "fixed"}},
			}, DocumentChanges: []WorkspaceDocumentChange{}},
			Command: &Command{Title: "Apply", Command: "server.apply"},
		},
	}
	if diff := gocmp.Diff(wantActions, gotActions); diff != "" {
		t.Fatalf("code actions mismatch (-want +got):\n%s", diff)
	}
	actionCalls := srv.codeActionCalls()
	if len(actionCalls) != 1 {
		t.Fatalf("code action calls = %d, want 1", len(actionCalls))
	}
	if actionCalls[0].TextDocument.URI != fileURI {
		t.Fatalf("code action URI = %q, want %q", actionCalls[0].TextDocument.URI, fileURI)
	}
	if actionCalls[0].Range != actionRange {
		t.Fatalf("code action range = %+v, want %+v", actionCalls[0].Range, actionRange)
	}
	if diff := gocmp.Diff([]protocol.CodeActionKind{actionKind}, actionCalls[0].Context.Only); diff != "" {
		t.Fatalf("code action only mismatch (-want +got):\n%s", diff)
	}
	if got := len(srv.codeActionResolveCalls()); got != 1 {
		t.Fatalf("codeAction/resolve calls = %d, want 1", got)
	}
}
