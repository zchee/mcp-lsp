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
	"fmt"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestWorkspaceSymbolsRejectUnsupportedCapabilityBeforeSync(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	srv := &fakeServer{}
	mgr := newFakeServerManager(t, srv, root)

	_, err := mgr.WorkspaceSymbols().Lookup(t.Context(), "go", "main")
	requireErrorContains(t, err, "workspace/symbol request is not supported")
	requireNoDocumentSync(t, srv)
}

func TestWorkspaceSymbolsLookupFlattensResultUnions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result protocol.WorkspaceSymbolResult
		want   []WorkspaceSymbol
	}{
		{
			name: "symbol information",
			result: protocol.SymbolInformationSlice{
				{
					Name:          "Handler",
					Kind:          protocol.SymbolKindFunction,
					ContainerName: new("server"),
					Location: protocol.Location{
						URI: uri.File("/workspace/server.go"),
						Range: protocol.Range{
							Start: protocol.Position{Line: 10, Character: 2},
							End:   protocol.Position{Line: 10, Character: 9},
						},
					},
				},
			},
			want: []WorkspaceSymbol{
				{
					Name:          "Handler",
					Kind:          fmt.Sprint(protocol.SymbolKindFunction),
					ContainerName: "server",
					URI:           uri.File("/workspace/server.go").String(),
					Range:         &NavigationRange{StartLine: 10, StartColumn: 2, EndLine: 10, EndColumn: 9},
				},
			},
		},
		{
			name: "workspace symbol location",
			result: protocol.WorkspaceSymbolSlice{
				{
					Name: "pkg",
					Kind: protocol.SymbolKindPackage,
					Location: &protocol.Location{
						URI: uri.File("/workspace/pkg"),
						Range: protocol.Range{
							Start: protocol.Position{Line: 3, Character: 0},
							End:   protocol.Position{Line: 3, Character: 7},
						},
					},
				},
			},
			want: []WorkspaceSymbol{
				{
					Name: "pkg",
					Kind: fmt.Sprint(protocol.SymbolKindPackage),
					URI:  uri.File("/workspace/pkg").String(),
					Range: &NavigationRange{
						StartLine:   3,
						StartColumn: 0,
						EndLine:     3,
						EndColumn:   7,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			srv := &fakeServer{
				capabilities: protocol.ServerCapabilities{WorkspaceSymbolProvider: &protocol.WorkspaceSymbolOptions{}},
				symbolResult: tt.result,
			}
			mgr := newFakeServerManager(t, srv, root)

			got, err := mgr.WorkspaceSymbols().Lookup(t.Context(), "go", "handler")
			if err != nil {
				t.Fatalf("WorkspaceSymbols.Lookup: %v", err)
			}
			if diff := gocmp.Diff(tt.want, got); diff != "" {
				t.Fatalf("workspace symbols mismatch (-want +got):\n%s", diff)
			}

			calls := srv.symbolCalls()
			if len(calls) != 1 {
				t.Fatalf("symbol calls = %d, want 1", len(calls))
			}
			if calls[0].Query != "handler" {
				t.Fatalf("symbol query = %q, want handler", calls[0].Query)
			}
		})
	}
}

func TestFlattenWorkspaceSymbolsSupportsURIOnlyLocations(t *testing.T) {
	t.Parallel()

	got := flattenWorkspaceSymbols(protocol.WorkspaceSymbolSlice{
		{
			Name:     "pkg",
			Kind:     protocol.SymbolKindPackage,
			Location: &protocol.LocationUriOnly{URI: uri.File("/workspace/pkg")},
		},
	})
	want := []WorkspaceSymbol{
		{
			Name: "pkg",
			Kind: fmt.Sprint(protocol.SymbolKindPackage),
			URI:  uri.File("/workspace/pkg").String(),
		},
	}
	if diff := gocmp.Diff(want, got); diff != "" {
		t.Fatalf("workspace symbols mismatch (-want +got):\n%s", diff)
	}
}
