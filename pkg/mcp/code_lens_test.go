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
	"go.lsp.dev/uri"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

type fakeCodeLensLooker struct {
	lenses     []lsp.CodeLens
	gotLang    string
	gotPath    string
	gotText    string
	gotResolve bool
	calls      int
}

func (f *fakeCodeLensLooker) Lookup(_ context.Context, lang, absPath, text string, resolve bool) ([]lsp.CodeLens, error) {
	f.calls++
	f.gotLang = lang
	f.gotPath = absPath
	f.gotText = text
	f.gotResolve = resolve
	return f.lenses, nil
}

func TestCodeLensHandlerReadsFileAndConvertsOutputRanges(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	looker := &fakeCodeLensLooker{
		lenses: []lsp.CodeLens{
			{
				Range:   lsp.NavigationRange{StartLine: 4, StartColumn: 0, EndLine: 4, EndColumn: 0},
				Command: &lsp.Command{Title: "Test", Command: "go.test"},
			},
		},
	}
	handler := codeLensHandler(looker, t.TempDir())

	_, out, err := handler(t.Context(), nil, CodeLensInput{File: path, Resolve: true})
	if err != nil {
		t.Fatalf("code lens handler: %v", err)
	}
	if looker.gotLang != "go" || looker.gotPath != path || looker.gotText != fileContent || !looker.gotResolve {
		t.Fatalf("Lookup args = lang:%q path:%q text:%q resolve:%v", looker.gotLang, looker.gotPath, looker.gotText, looker.gotResolve)
	}
	want := CodeLensOutput{
		File: path,
		URI:  uri.File(path).String(),
		Lenses: []CodeLensItem{
			{
				Range:   DefinitionRangeItem{StartLine: 5, StartColumn: 1, EndLine: 5, EndColumn: 1},
				Command: &CommandItem{Title: "Test", Command: "go.test"},
			},
		},
	}
	if diff := gocmp.Diff(want, out); diff != "" {
		t.Fatalf("code lens output mismatch (-want +got):\n%s", diff)
	}
}
