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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestApplyWorkspaceEditHandlerAppliesTextChanges(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("Hello world\n"), 0o644); err != nil {
		t.Fatalf("write input file: %v", err)
	}

	handler := applyWorkspaceEditHandler(root)
	_, out, err := handler(t.Context(), nil, ApplyWorkspaceEditInput{
		Edit: protocol.WorkspaceEdit{
			Changes: map[uri.URI][]protocol.TextEdit{
				uri.File(path): {
					{
						Range: protocol.Range{
							Start: protocol.Position{Line: 0, Character: 0},
							End:   protocol.Position{Line: 0, Character: 5},
						},
						NewText: "Hi",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("handler error = %v", err)
	}
	if !out.Applied {
		t.Fatalf("applied = %v, want true", out.Applied)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(got) != "Hi world\n" {
		t.Fatalf("file text = %q, want %q", string(got), "Hi world\n")
	}
}

func TestApplyWorkspaceEditHandlerRejectsDisallowedCreate(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	createPath := filepath.Join(root, "created.txt")
	handler := applyWorkspaceEditHandler(root)
	_, out, err := handler(t.Context(), nil, ApplyWorkspaceEditInput{
		Edit: protocol.WorkspaceEdit{
			DocumentChanges: []protocol.DocumentChange{
				&protocol.CreateFile{URI: uri.File(createPath)},
			},
		},
	})
	if err != nil {
		t.Fatalf("handler error = %v", err)
	}
	if out.Applied {
		t.Fatal("applied = true, want false")
	}
	if out.FailureReason == nil || !strings.Contains(*out.FailureReason, "disabled by policy") {
		t.Fatalf("failure reason = %v, want disabled by policy", out.FailureReason)
	}
	if _, err := os.Stat(createPath); err == nil {
		t.Fatal("created file should not exist when create is disabled")
	}
}

func TestApplyWorkspaceEditHandlerRejectsUnsupportedSnippetTextEdit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	handler := applyWorkspaceEditHandler(root)
	_, _, err := handler(t.Context(), nil, ApplyWorkspaceEditInput{
		Edit: protocol.WorkspaceEdit{
			DocumentChanges: []protocol.DocumentChange{
				&protocol.TextDocumentEdit{
					TextDocument: protocol.OptionalVersionedTextDocumentIdentifier{
						TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: uri.File(path)},
					},
					Edits: []protocol.TextDocumentEditElement{
						&protocol.SnippetTextEdit{
							Range: protocol.Range{
								Start: protocol.Position{Line: 0, Character: 0},
								End:   protocol.Position{Line: 0, Character: 1},
							},
							Snippet: protocol.StringValue{Value: "placeholder"},
						},
					},
				},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported snippet text edit") {
		t.Fatalf("handler error = %v, want unsupported snippet text edit", err)
	}
}

func TestApplyWorkspaceEditHandlerSupportsVersionChecks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write input file: %v", err)
	}

	handler := applyWorkspaceEditHandler(root)
	_, out, err := handler(t.Context(), nil, ApplyWorkspaceEditInput{
		CurrentVersions: map[string]uint32{
			string(uri.File(path)): 3,
		},
		Edit: protocol.WorkspaceEdit{
			DocumentChanges: []protocol.DocumentChange{
				&protocol.TextDocumentEdit{
					TextDocument: protocol.OptionalVersionedTextDocumentIdentifier{
						TextDocumentIdentifier: protocol.TextDocumentIdentifier{URI: uri.File(path)},
						Version:                int32Ptr(4),
					},
					Edits: []protocol.TextDocumentEditElement{
						&protocol.TextEdit{
							Range: protocol.Range{
								Start: protocol.Position{Line: 0, Character: 0},
								End:   protocol.Position{Line: 0, Character: 5},
							},
							NewText: "HELLO",
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("handler error = %v", err)
	}
	if out.Applied {
		t.Fatalf("applied = %v, want false", out.Applied)
	}
	if out.FailureReason == nil || !strings.Contains(*out.FailureReason, "version mismatch") {
		t.Fatalf("failure reason = %v, want version mismatch", out.FailureReason)
	}
}

func int32Ptr(v int32) *int32 {
	return &v
}
