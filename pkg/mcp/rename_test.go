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
	"strings"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

type fakeRenamer struct {
	edit       lsp.WorkspaceEdit
	gotLang    string
	gotPath    string
	gotText    string
	gotPos     protocol.Position
	gotNewName string
	calls      int
}

func (f *fakeRenamer) Preview(_ context.Context, lang, absPath, text string, pos protocol.Position, newName string) (lsp.WorkspaceEdit, error) {
	f.calls++
	f.gotLang = lang
	f.gotPath = absPath
	f.gotText = text
	f.gotPos = pos
	f.gotNewName = newName
	return f.edit, nil
}

func TestRenameHandlerRequiresNewNameBeforeLookup(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	renamer := &fakeRenamer{}
	handler := renameHandler(renamer, t.TempDir(), testResolver(t, "go", "python", "rust"))

	_, _, err := handler(t.Context(), nil, RenameInput{File: path, Line: 3, Column: 5})
	if err == nil || !strings.Contains(err.Error(), "newName is required") {
		t.Fatalf("empty newName error = %v, want required error", err)
	}
	if renamer.calls != 0 {
		t.Fatalf("Preview calls = %d, want 0 for invalid input", renamer.calls)
	}
}

func TestRenameHandlerInfersLanguageAndReturnsWorkspaceEditPreview(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	fileURI := uri.File(path)
	renamer := &fakeRenamer{
		edit: lsp.WorkspaceEdit{Changes: map[string][]lsp.WorkspaceTextEdit{
			fileURI.String(): {{Range: lsp.NavigationRange{StartLine: 2, StartColumn: 4, EndLine: 2, EndColumn: 8}, NewText: "Renamed"}},
		}},
	}
	handler := renameHandler(renamer, t.TempDir(), testResolver(t, "go", "python", "rust"))

	_, out, err := handler(t.Context(), nil, RenameInput{File: path, Line: 3, Column: 5, NewName: "Renamed"})
	if err != nil {
		t.Fatalf("rename handler: %v", err)
	}
	if renamer.gotLang != "go" {
		t.Fatalf("Preview language = %q, want go", renamer.gotLang)
	}
	if renamer.gotPath != path || renamer.gotText != fileContent {
		t.Fatalf("Preview path/text = %q/%q, want %q/file contents", renamer.gotPath, renamer.gotText, path)
	}
	if want := (protocol.Position{Line: 2, Character: 4}); renamer.gotPos != want {
		t.Fatalf("Preview position = %+v, want %+v", renamer.gotPos, want)
	}
	if renamer.gotNewName != "Renamed" {
		t.Fatalf("Preview newName = %q, want Renamed", renamer.gotNewName)
	}
	wantEdit := protocol.WorkspaceEdit{Changes: map[uri.URI][]protocol.TextEdit{
		fileURI: {{Range: protocol.Range{Start: protocol.Position{Line: 2, Character: 4}, End: protocol.Position{Line: 2, Character: 8}}, NewText: "Renamed"}},
	}}
	if diff := gocmp.Diff(WorkspaceEditPreviewOutput{File: path, URI: fileURI.String(), Edit: wantEdit}, out); diff != "" {
		t.Fatalf("rename output mismatch (-want +got):\n%s", diff)
	}
}
