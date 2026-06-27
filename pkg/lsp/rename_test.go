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

func TestRenameRejectsUnsupportedCapabilityBeforeSync(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	srv := &fakeServer{}
	mgr := newFakeServerManager(t, srv, root)

	_, err := mgr.Rename().Preview(t.Context(), "go", path, "package main\n", protocol.Position{}, "renamed")
	requireErrorContains(t, err, "rename request is not supported")
	requireNoDocumentSync(t, srv)
}

func TestRenameReturnsWorkspaceEditPreview(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	fileURI := uri.File(path)
	renameRange := protocol.Range{Start: protocol.Position{Line: 2, Character: 4}, End: protocol.Position{Line: 2, Character: 8}}
	srv := &fakeServer{
		capabilities: protocol.ServerCapabilities{RenameProvider: &protocol.RenameOptions{}},
		renameEdit: &protocol.WorkspaceEdit{
			Changes: map[uri.URI][]protocol.TextEdit{
				fileURI: {
					{Range: renameRange, NewText: "Renamed"},
				},
			},
		},
	}
	mgr := newFakeServerManager(t, srv, root)

	renamePos := protocol.Position{Line: 2, Character: 4}
	gotRename, err := mgr.Rename().Preview(t.Context(), "go", path, "package main\n", renamePos, "Renamed")
	if err != nil {
		t.Fatalf("Rename.Preview: %v", err)
	}
	wantRename := WorkspaceEdit{Changes: map[string][]WorkspaceTextEdit{
		fileURI.String(): {{Range: NavigationRange{StartLine: 2, StartColumn: 4, EndLine: 2, EndColumn: 8}, NewText: "Renamed"}},
	}, DocumentChanges: []WorkspaceDocumentChange{}}
	if diff := gocmp.Diff(wantRename, gotRename); diff != "" {
		t.Fatalf("rename workspace edit mismatch (-want +got):\n%s", diff)
	}

	renameCalls := srv.renameCalls()
	if len(renameCalls) != 1 {
		t.Fatalf("rename calls = %d, want 1", len(renameCalls))
	}
	if renameCalls[0].Position != renamePos {
		t.Fatalf("rename position = %+v, want %+v", renameCalls[0].Position, renamePos)
	}
	if renameCalls[0].NewName != "Renamed" {
		t.Fatalf("rename newName = %q, want Renamed", renameCalls[0].NewName)
	}
}
