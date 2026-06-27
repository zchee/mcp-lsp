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

func TestFormattingRejectsUnsupportedCapabilitiesBeforeSync(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	srv := &fakeManagerServer{}
	mgr := newFeatureManager(t, srv, root)
	rng := protocol.Range{Start: protocol.Position{Line: 0, Character: 0}, End: protocol.Position{Line: 0, Character: 1}}

	_, err := mgr.Formatting().Format(t.Context(), "go", path, "package main\n", protocol.FormattingOptions{})
	requireErrorContains(t, err, "formatting request is not supported")
	requireNoFeatureSync(t, srv)

	_, err = mgr.Formatting().RangeFormat(t.Context(), "go", path, "package main\n", rng, protocol.FormattingOptions{})
	requireErrorContains(t, err, "range formatting request is not supported")
	requireNoFeatureSync(t, srv)
}

func TestFormattingAndRangeFormattingReturnWorkspaceEditPreviews(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	fileURI := uri.File(path)
	formatRange := protocol.Range{Start: protocol.Position{Line: 0, Character: 0}, End: protocol.Position{Line: 0, Character: 0}}
	rangeFormatRange := protocol.Range{Start: protocol.Position{Line: 1, Character: 0}, End: protocol.Position{Line: 1, Character: 7}}
	srv := &fakeManagerServer{
		capabilities: protocol.ServerCapabilities{
			DocumentFormattingProvider:      protocol.Boolean(true),
			DocumentRangeFormattingProvider: &protocol.DocumentRangeFormattingOptions{},
		},
		formattingEdits: []protocol.TextEdit{
			{Range: formatRange, NewText: "// formatted\n"},
		},
		rangeFormattingEdits: []protocol.TextEdit{
			{Range: rangeFormatRange, NewText: "renamed"},
		},
	}
	mgr := newFeatureManager(t, srv, root)

	formatOptions := protocol.FormattingOptions{TabSize: 8, InsertSpaces: false}
	gotFormat, err := mgr.Formatting().Format(t.Context(), "go", path, "package main\n", formatOptions)
	if err != nil {
		t.Fatalf("Formatting.Format: %v", err)
	}
	wantFormat := WorkspaceEdit{Changes: map[string][]WorkspaceTextEdit{
		fileURI.String(): {{Range: NavigationRange{StartLine: 0, StartColumn: 0, EndLine: 0, EndColumn: 0}, NewText: "// formatted\n"}},
	}}
	if diff := gocmp.Diff(wantFormat, gotFormat); diff != "" {
		t.Fatalf("format workspace edit mismatch (-want +got):\n%s", diff)
	}

	gotRangeFormat, err := mgr.Formatting().RangeFormat(t.Context(), "go", path, "package main\n", rangeFormatRange, formatOptions)
	if err != nil {
		t.Fatalf("Formatting.RangeFormat: %v", err)
	}
	wantRangeFormat := WorkspaceEdit{Changes: map[string][]WorkspaceTextEdit{
		fileURI.String(): {{Range: NavigationRange{StartLine: 1, StartColumn: 0, EndLine: 1, EndColumn: 7}, NewText: "renamed"}},
	}}
	if diff := gocmp.Diff(wantRangeFormat, gotRangeFormat); diff != "" {
		t.Fatalf("range format workspace edit mismatch (-want +got):\n%s", diff)
	}

	formatCalls := srv.formattingCalls()
	if len(formatCalls) != 1 {
		t.Fatalf("format calls = %d, want 1", len(formatCalls))
	}
	if formatCalls[0].TextDocument.URI != fileURI {
		t.Fatalf("format URI = %q, want %q", formatCalls[0].TextDocument.URI, fileURI)
	}
	if diff := gocmp.Diff(formatOptions, formatCalls[0].Options); diff != "" {
		t.Fatalf("format options mismatch (-want +got):\n%s", diff)
	}
	rangeCalls := srv.rangeFormattingCalls()
	if len(rangeCalls) != 1 {
		t.Fatalf("range format calls = %d, want 1", len(rangeCalls))
	}
	if rangeCalls[0].Range != rangeFormatRange {
		t.Fatalf("range format range = %+v, want %+v", rangeCalls[0].Range, rangeFormatRange)
	}
}
