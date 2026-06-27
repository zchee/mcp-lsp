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
	"strings"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestWorkspaceEditFromProtocolAndBack(t *testing.T) {
	t.Parallel()

	text := uri.File("/tmp/workspace/main.go").String()
	annotationID := "annotation-1"
	description := "replace hello"
	needsConfirmation := true
	version := int32(7)

	rawEdit := protocol.WorkspaceEdit{
		Changes: map[uri.URI][]protocol.TextEdit{
			uri.File("/tmp/workspace/main.go"): {
				{
					Range: protocol.Range{
						Start: protocol.Position{Line: 0, Character: 0},
						End:   protocol.Position{Line: 0, Character: 5},
					},
					NewText: "hey",
				},
			},
		},
		DocumentChanges: []protocol.DocumentChange{
			&protocol.TextDocumentEdit{
				TextDocument: protocol.OptionalVersionedTextDocumentIdentifier{
					TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: uri.File("/tmp/workspace/main.go")},
					Version:                &version,
				},
				Edits: []protocol.TextDocumentEditElement{
					&protocol.AnnotatedTextEdit{
						Range: protocol.Range{
							Start: protocol.Position{Line: 1, Character: 0},
							End:   protocol.Position{Line: 1, Character: 2},
						},
						NewText:      "yo",
						AnnotationID: protocol.ChangeAnnotationIdentifier(annotationID),
					},
				},
			},
			&protocol.CreateFile{
				URI: uri.File("/tmp/workspace/new.txt"),
				Options: &protocol.CreateFileOptions{
					Overwrite:      new(true),
					IgnoreIfExists: new(false),
				},
			},
		},
		ChangeAnnotations: map[protocol.ChangeAnnotationIdentifier]protocol.ChangeAnnotation{
			protocol.ChangeAnnotationIdentifier(annotationID): {
				Label:             "replace with short string",
				NeedsConfirmation: &needsConfirmation,
				Description:       &description,
			},
		},
	}

	dto, err := WorkspaceEditFromProtocol(rawEdit)
	if err != nil {
		t.Fatalf("WorkspaceEditFromProtocol() returned error: %v", err)
	}

	if len(dto.Changes) != len(rawEdit.Changes) {
		t.Fatalf("Changes length = %d, want %d", len(dto.Changes), len(rawEdit.Changes))
	}
	rawEditTextChanges := rawEdit.Changes[uri.File("/tmp/workspace/main.go")]
	gotTextChanges := dto.Changes[text]
	if len(gotTextChanges) != len(rawEditTextChanges) {
		t.Fatalf("text change count = %d, want %d", len(gotTextChanges), len(rawEditTextChanges))
	}
	if gotTextChanges[0].Range != (NavigationRange{0, 0, 0, 5}) {
		t.Errorf("text edit range = %#v, want %#v", gotTextChanges[0].Range, NavigationRange{0, 0, 0, 5})
	}
	if gotTextChanges[0].NewText != "hey" {
		t.Errorf("text edit newText = %q, want %q", gotTextChanges[0].NewText, "hey")
	}

	gotDocumentChange := dto.DocumentChanges[0]
	if gotDocumentChange.TextDocumentEdit == nil {
		t.Fatalf("first document change has no text document edit")
	}
	gotTextDocumentEdit := gotDocumentChange.TextDocumentEdit
	if gotTextDocumentEdit.TextDocument.URI != text {
		t.Errorf("document change URI = %q, want %q", gotTextDocumentEdit.TextDocument.URI, text)
	}
	if gotTextDocumentEdit.TextDocument.Version == nil || *gotTextDocumentEdit.TextDocument.Version != uint32(version) {
		t.Fatalf("document change version = %v, want %d", gotTextDocumentEdit.TextDocument.Version, uint32(version))
	}
	if len(gotTextDocumentEdit.Edits) != 1 {
		t.Fatalf("document change edit count = %d, want 1", len(gotTextDocumentEdit.Edits))
	}
	if gotTextDocumentEdit.Edits[0].AnnotationID == nil {
		t.Fatal("annotated text edit lost annotation id")
	}

	if got := dto.ChangeAnnotations[annotationID]; got.Label != "replace with short string" {
		t.Fatalf("annotation label = %q, want %q", got.Label, "replace with short string")
	}

	wireEdit, err := WorkspaceEditToProtocol(dto)
	if err != nil {
		t.Fatalf("WorkspaceEditToProtocol() returned error: %v", err)
	}

	if len(wireEdit.DocumentChanges) != len(rawEdit.DocumentChanges) {
		t.Fatalf("wire document changes len = %d, want %d", len(wireEdit.DocumentChanges), len(rawEdit.DocumentChanges))
	}
	if _, ok := wireEdit.DocumentChanges[1].(*protocol.CreateFile); !ok {
		t.Errorf("wire document change[1] = %#T, want *protocol.CreateFile", wireEdit.DocumentChanges[1])
	}

	wantChanges := map[uri.URI][]protocol.TextEdit{
		uri.File("/tmp/workspace/main.go"): {
			{
				Range: protocol.Range{
					Start: protocol.Position{Line: 0, Character: 0},
					End:   protocol.Position{Line: 0, Character: 5},
				},
				NewText: "hey",
			},
		},
	}
	if diff := gocmp.Diff(wantChanges, wireEdit.Changes); diff != "" {
		t.Errorf("workspace edit changes mismatch (-want +got):\n%s", diff)
	}
	wantAnnotations := map[protocol.ChangeAnnotationIdentifier]protocol.ChangeAnnotation{
		protocol.ChangeAnnotationIdentifier(annotationID): {
			Label:             "replace with short string",
			NeedsConfirmation: &needsConfirmation,
			Description:       &description,
		},
	}
	if diff := gocmp.Diff(wantAnnotations, wireEdit.ChangeAnnotations); diff != "" {
		t.Errorf("workspace edit annotations mismatch (-want +got):\n%s", diff)
	}
}

func TestWorkspaceEditFromProtocolRejectsUnsupportedVariant(t *testing.T) {
	t.Parallel()

	problem := protocol.WorkspaceEdit{
		DocumentChanges: []protocol.DocumentChange{
			&protocol.TextDocumentEdit{
				TextDocument: protocol.OptionalVersionedTextDocumentIdentifier{
					TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: uri.File("/tmp/workspace/main.go")},
				},
				Edits: []protocol.TextDocumentEditElement{
					&protocol.SnippetTextEdit{
						Range: protocol.Range{
							Start: protocol.Position{Line: 0, Character: 0},
							End:   protocol.Position{Line: 0, Character: 5},
						},
						Snippet: protocol.StringValue{Value: "ignored"},
					},
				},
			},
		},
	}

	_, err := WorkspaceEditFromProtocol(problem)
	if err == nil {
		t.Fatalf("expected unsupported snippet edit error, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "unsupported snippet text edit") {
		t.Fatalf("error = %q, want contains unsupported snippet text edit", got)
	}
}

func TestWorkspaceEditFromProtocolRejectsNegativeVersion(t *testing.T) {
	t.Parallel()

	raw := protocol.WorkspaceEdit{
		DocumentChanges: []protocol.DocumentChange{
			&protocol.TextDocumentEdit{
				TextDocument: protocol.OptionalVersionedTextDocumentIdentifier{
					TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: uri.File("/tmp/workspace/main.go")},
					Version:                new(int32(-7)),
				},
				Edits: []protocol.TextDocumentEditElement{
					&protocol.TextEdit{
						Range: protocol.Range{
							Start: protocol.Position{Line: 0, Character: 0},
							End:   protocol.Position{Line: 0, Character: 1},
						},
						NewText: "x",
					},
				},
			},
		},
	}

	_, err := WorkspaceEditFromProtocol(raw)
	if err == nil {
		t.Fatalf("expected version rejection error, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "negative text document version") {
		t.Fatalf("error = %q, want contains negative text document version", got)
	}
}

func TestWorkspaceEditFromProtocolRejectsNonFileDocumentChangeURIs(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		edit protocol.WorkspaceEdit
		want string
	}{
		"text document edit": {
			edit: protocol.WorkspaceEdit{DocumentChanges: []protocol.DocumentChange{
				&protocol.TextDocumentEdit{
					TextDocument: protocol.OptionalVersionedTextDocumentIdentifier{
						TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: uri.URI("http://example.com/main.go")},
					},
				},
			}},
			want: "text document edit URI must be file",
		},
		"create file": {
			edit: protocol.WorkspaceEdit{DocumentChanges: []protocol.DocumentChange{
				&protocol.CreateFile{URI: uri.URI("http://example.com/new.go")},
			}},
			want: "create operation URI must be file",
		},
		"rename file": {
			edit: protocol.WorkspaceEdit{DocumentChanges: []protocol.DocumentChange{
				&protocol.RenameFile{OldURI: uri.File("/tmp/old.go"), NewURI: uri.URI("http://example.com/new.go")},
			}},
			want: "rename operation new URI must be file",
		},
		"delete file": {
			edit: protocol.WorkspaceEdit{DocumentChanges: []protocol.DocumentChange{
				&protocol.DeleteFile{URI: uri.URI("http://example.com/main.go")},
			}},
			want: "delete operation URI must be file",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := WorkspaceEditFromProtocol(tt.edit)
			if err == nil {
				t.Fatalf("WorkspaceEditFromProtocol accepted non-file URI")
			}
			if got := err.Error(); !strings.Contains(got, tt.want) {
				t.Fatalf("error = %q, want contains %q", got, tt.want)
			}
		})
	}
}

func TestWorkspaceEditToProtocolRejectsInvalidURI(t *testing.T) {
	t.Parallel()

	dto := WorkspaceEdit{
		Changes: map[string][]WorkspaceTextEdit{
			":/invalid": {
				{
					Range: NavigationRange{
						StartLine: 0, StartColumn: 0, EndLine: 0, EndColumn: 1,
					},
					NewText: "x",
				},
			},
		},
	}

	_, err := WorkspaceEditToProtocol(dto)
	if err == nil {
		t.Fatalf("expected invalid URI error, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "invalid changes URI") {
		t.Fatalf("error = %q, want contains invalid changes URI", got)
	}
}
