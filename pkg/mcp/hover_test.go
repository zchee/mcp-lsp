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
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

type fakeHoverLooker struct {
	hover   *lsp.HoverResult
	gotLang string
	gotPath string
	gotText string
	gotPos  protocol.Position
	calls   int
}

func (f *fakeHoverLooker) Lookup(_ context.Context, lang, absPath, text string, pos protocol.Position) (*lsp.HoverResult, error) {
	f.calls++
	f.gotLang = lang
	f.gotPath = absPath
	f.gotText = text
	f.gotPos = pos
	return f.hover, nil
}

func TestHoverHandlerReadsFileInfersLanguageAndConvertsCoordinates(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	hoverRange := lsp.NavigationRange{StartLine: 1, StartColumn: 2, EndLine: 1, EndColumn: 5}
	looker := &fakeHoverLooker{
		hover: &lsp.HoverResult{Kind: "markdown", Value: "**doc**", Range: &hoverRange},
	}
	handler := hoverHandler(looker, t.TempDir(), testResolver(t, "go", "python", "rust"))

	_, out, err := handler(t.Context(), nil, HoverInput{File: path, Line: 3, Column: 5})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if looker.calls != 1 {
		t.Fatalf("Lookup calls = %d, want 1", looker.calls)
	}
	if looker.gotLang != "go" {
		t.Fatalf("Lookup language = %q, want go", looker.gotLang)
	}
	if looker.gotPath != path {
		t.Fatalf("Lookup path = %q, want %q", looker.gotPath, path)
	}
	if looker.gotText != fileContent {
		t.Fatalf("Lookup text = %q, want file contents", looker.gotText)
	}
	if want := (protocol.Position{Line: 2, Character: 4}); looker.gotPos != want {
		t.Fatalf("Lookup position = %+v, want %+v", looker.gotPos, want)
	}

	want := HoverOutput{
		File: path,
		URI:  string(uri.File(path)),
		Hover: &HoverItem{
			Kind:  "markdown",
			Value: "**doc**",
			Range: &DefinitionRangeItem{StartLine: 2, StartColumn: 3, EndLine: 2, EndColumn: 6},
		},
	}
	if diff := gocmp.Diff(want, out); diff != "" {
		t.Fatalf("hover output mismatch (-want +got):\n%s", diff)
	}
}
