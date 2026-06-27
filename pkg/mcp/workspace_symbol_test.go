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

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

type fakeWorkspaceSymbolLooker struct {
	symbols  []lsp.WorkspaceSymbol
	gotLang  string
	gotQuery string
	calls    int
}

func (f *fakeWorkspaceSymbolLooker) Lookup(_ context.Context, lang, query string) ([]lsp.WorkspaceSymbol, error) {
	f.calls++
	f.gotLang = lang
	f.gotQuery = query
	return f.symbols, nil
}

func TestWorkspaceSymbolHandlerDefaultsLanguageAndConvertsRanges(t *testing.T) {
	t.Parallel()

	symbolRange := lsp.NavigationRange{StartLine: 4, StartColumn: 1, EndLine: 4, EndColumn: 8}
	looker := &fakeWorkspaceSymbolLooker{
		symbols: []lsp.WorkspaceSymbol{
			{Name: "Handler", Kind: "12", ContainerName: "server", URI: "file:///workspace/server.go", Range: &symbolRange},
			{Name: "pkg", Kind: "4", URI: "file:///workspace/pkg"},
		},
	}
	handler := workspaceSymbolHandler(looker)

	_, out, err := handler(t.Context(), nil, WorkspaceSymbolInput{Query: "handler"})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if looker.gotLang != "go" {
		t.Fatalf("Lookup language = %q, want go", looker.gotLang)
	}
	if looker.gotQuery != "handler" {
		t.Fatalf("Lookup query = %q, want handler", looker.gotQuery)
	}

	want := WorkspaceSymbolOutput{Symbols: []WorkspaceSymbolItem{
		{
			Name:          "Handler",
			Kind:          "12",
			ContainerName: "server",
			URI:           "file:///workspace/server.go",
			Range:         &DefinitionRangeItem{StartLine: 5, StartColumn: 2, EndLine: 5, EndColumn: 9},
		},
		{Name: "pkg", Kind: "4", URI: "file:///workspace/pkg"},
	}}
	if diff := gocmp.Diff(want, out); diff != "" {
		t.Fatalf("workspace symbol output mismatch (-want +got):\n%s", diff)
	}
}
