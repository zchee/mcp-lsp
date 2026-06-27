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

	"github.com/go-json-experiment/json"
	gocmp "github.com/google/go-cmp/cmp"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestHoverRejectsUnsupportedCapabilityBeforeSync(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	srv := &fakeServer{}
	mgr := newFakeServerManager(t, srv, root)

	_, err := mgr.Hover().Lookup(t.Context(), "go", path, "package main\n", protocol.Position{})
	requireErrorContains(t, err, "hover request is not supported")
	requireNoDocumentSync(t, srv)
}

func TestHoverLookupFlattensMarkupAndRecordsWireParams(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	hoverRange := protocol.Range{
		Start: protocol.Position{Line: 1, Character: 2},
		End:   protocol.Position{Line: 1, Character: 5},
	}
	srv := &fakeServer{
		capabilities: protocol.ServerCapabilities{HoverProvider: protocol.Boolean(true)},
		hoverResult: &protocol.Hover{
			Contents: &protocol.MarkupContent{Kind: protocol.MarkupKindMarkdown, Value: "**doc**"},
			Range:    &hoverRange,
		},
	}
	mgr := newFakeServerManager(t, srv, root)
	pos := protocol.Position{Line: 3, Character: 4}

	got, err := mgr.Hover().Lookup(t.Context(), "go", path, "package main\n", pos)
	if err != nil {
		t.Fatalf("Hover.Lookup: %v", err)
	}

	want := &HoverResult{Kind: "markdown", Value: "**doc**", Range: &NavigationRange{StartLine: 1, StartColumn: 2, EndLine: 1, EndColumn: 5}}
	if diff := gocmp.Diff(want, got); diff != "" {
		t.Fatalf("hover result mismatch (-want +got):\n%s", diff)
	}

	calls := srv.hoverCalls()
	if len(calls) != 1 {
		t.Fatalf("hover calls = %d, want 1", len(calls))
	}
	if gotURI := calls[0].TextDocument.URI; gotURI != uri.File(path) {
		t.Fatalf("hover URI = %q, want %q", gotURI, uri.File(path))
	}
	if calls[0].Position != pos {
		t.Fatalf("hover position = %+v, want %+v", calls[0].Position, pos)
	}
	opened := srv.openedDocs()
	if len(opened) != 1 {
		t.Fatalf("didOpen calls = %d, want 1", len(opened))
	}
	if opened[0].TextDocument.LanguageID != protocol.LanguageKindGo {
		t.Fatalf("didOpen languageID = %q, want %q", opened[0].TextDocument.LanguageID, protocol.LanguageKindGo)
	}
}

func TestFlattenHoverSupportsLegacyMarkedStringWireShapes(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		raw  string
		want *HoverResult
	}{
		"marked string with language": {
			raw:  `{"contents":{"language":"go","value":"func main()"}}`,
			want: &HoverResult{Kind: "plaintext", Value: "```go\nfunc main()\n```"},
		},
		"marked string slice": {
			raw:  `{"contents":["plain",{"language":"go","value":"func main()"}]}`,
			want: &HoverResult{Kind: "plaintext", Value: "plain\n\n```go\nfunc main()\n```"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var hover protocol.Hover
			if err := json.Unmarshal([]byte(tt.raw), &hover); err != nil {
				t.Fatalf("unmarshal hover: %v", err)
			}
			if diff := gocmp.Diff(tt.want, flattenHover(&hover)); diff != "" {
				t.Fatalf("hover mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
