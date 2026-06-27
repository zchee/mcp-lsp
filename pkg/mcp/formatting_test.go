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

type fakeFormatter struct {
	formatEdit      lsp.WorkspaceEdit
	rangeEdit       lsp.WorkspaceEdit
	formatCalls     int
	rangeCalls      int
	gotFormatLang   string
	gotFormatPath   string
	gotFormatText   string
	gotFormatOpts   protocol.FormattingOptions
	gotRangeLang    string
	gotRangePath    string
	gotRangeText    string
	gotRange        protocol.Range
	gotRangeOptions protocol.FormattingOptions
}

func (f *fakeFormatter) Format(_ context.Context, lang, absPath, text string, options protocol.FormattingOptions) (lsp.WorkspaceEdit, error) {
	f.formatCalls++
	f.gotFormatLang = lang
	f.gotFormatPath = absPath
	f.gotFormatText = text
	f.gotFormatOpts = options
	return f.formatEdit, nil
}

func (f *fakeFormatter) RangeFormat(_ context.Context, lang, absPath, text string, rng protocol.Range, options protocol.FormattingOptions) (lsp.WorkspaceEdit, error) {
	f.rangeCalls++
	f.gotRangeLang = lang
	f.gotRangePath = absPath
	f.gotRangeText = text
	f.gotRange = rng
	f.gotRangeOptions = options
	return f.rangeEdit, nil
}

func TestFormattingHandlerReadsFileAndConvertsInputs(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	path := filepath.Join(workspace, "main.go")
	writeFile(t, path, fileContent)
	fileURI := uri.File(path)
	insertSpaces := false
	formatter := &fakeFormatter{
		formatEdit: lsp.WorkspaceEdit{Changes: map[string][]lsp.WorkspaceTextEdit{
			fileURI.String(): {{Range: lsp.NavigationRange{StartLine: 0, StartColumn: 0, EndLine: 0, EndColumn: 0}, NewText: "// formatted\n"}},
		}},
	}

	handler := formattingHandler(formatter, workspace)
	_, out, err := handler(t.Context(), nil, FormattingInput{File: "main.go", Language: "rust", TabSize: 2, InsertSpaces: &insertSpaces})
	if err != nil {
		t.Fatalf("formatting handler: %v", err)
	}
	if formatter.gotFormatLang != "rust" {
		t.Fatalf("Format language = %q, want rust", formatter.gotFormatLang)
	}
	if formatter.gotFormatPath != path || formatter.gotFormatText != fileContent {
		t.Fatalf("Format path/text = %q/%q, want %q/file contents", formatter.gotFormatPath, formatter.gotFormatText, path)
	}
	wantOptions := protocol.FormattingOptions{TabSize: 2, InsertSpaces: false}
	if diff := gocmp.Diff(wantOptions, formatter.gotFormatOpts); diff != "" {
		t.Fatalf("Format options mismatch (-want +got):\n%s", diff)
	}
	wantEdit := protocol.WorkspaceEdit{Changes: map[uri.URI][]protocol.TextEdit{
		fileURI: {{Range: protocol.Range{Start: protocol.Position{}, End: protocol.Position{}}, NewText: "// formatted\n"}},
	}}
	if diff := gocmp.Diff(WorkspaceEditPreviewOutput{File: path, URI: fileURI.String(), Edit: wantEdit}, out); diff != "" {
		t.Fatalf("formatting output mismatch (-want +got):\n%s", diff)
	}
}

func TestRangeFormattingHandlerReadsFileAndConvertsInputs(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	path := filepath.Join(workspace, "main.go")
	writeFile(t, path, fileContent)
	fileURI := uri.File(path)
	formatter := &fakeFormatter{
		rangeEdit: lsp.WorkspaceEdit{Changes: map[string][]lsp.WorkspaceTextEdit{
			fileURI.String(): {{Range: lsp.NavigationRange{StartLine: 1, StartColumn: 0, EndLine: 1, EndColumn: 7}, NewText: "renamed"}},
		}},
	}

	handler := rangeFormattingHandler(formatter, workspace)
	_, out, err := handler(t.Context(), nil, RangeFormattingInput{
		File:        "main.go",
		StartLine:   2,
		StartColumn: 1,
		EndLine:     2,
		EndColumn:   8,
	})
	if err != nil {
		t.Fatalf("range formatting handler: %v", err)
	}
	if formatter.gotRangeLang != "go" {
		t.Fatalf("RangeFormat language = %q, want go", formatter.gotRangeLang)
	}
	if formatter.gotRangePath != path || formatter.gotRangeText != fileContent {
		t.Fatalf("RangeFormat path/text = %q/%q, want %q/file contents", formatter.gotRangePath, formatter.gotRangeText, path)
	}
	wantRange := protocol.Range{Start: protocol.Position{Line: 1, Character: 0}, End: protocol.Position{Line: 1, Character: 7}}
	if formatter.gotRange != wantRange {
		t.Fatalf("RangeFormat range = %+v, want %+v", formatter.gotRange, wantRange)
	}
	wantOptions := protocol.FormattingOptions{TabSize: 4, InsertSpaces: true}
	if diff := gocmp.Diff(wantOptions, formatter.gotRangeOptions); diff != "" {
		t.Fatalf("RangeFormat options mismatch (-want +got):\n%s", diff)
	}
	wantEdit := protocol.WorkspaceEdit{Changes: map[uri.URI][]protocol.TextEdit{
		fileURI: {{Range: wantRange, NewText: "renamed"}},
	}}
	if diff := gocmp.Diff(WorkspaceEditPreviewOutput{File: path, URI: fileURI.String(), Edit: wantEdit}, out); diff != "" {
		t.Fatalf("range formatting output mismatch (-want +got):\n%s", diff)
	}
}

func TestRangeFormattingHandlerValidatesRangeBeforeFileIO(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "missing.go")
	formatter := &fakeFormatter{}
	handler := rangeFormattingHandler(formatter, t.TempDir())

	_, _, err := handler(t.Context(), nil, RangeFormattingInput{
		File:        missing,
		StartLine:   0,
		StartColumn: 1,
		EndLine:     1,
		EndColumn:   1,
	})
	if err == nil || !strings.Contains(err.Error(), "line must be greater than zero") {
		t.Fatalf("range formatting error = %v, want line validation error", err)
	}
	if formatter.rangeCalls != 0 {
		t.Fatalf("RangeFormat calls = %d, want 0", formatter.rangeCalls)
	}
}
