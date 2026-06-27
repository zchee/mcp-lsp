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

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

type workspaceSymbolLooker interface {
	Lookup(ctx context.Context, lang, query string) ([]lsp.WorkspaceSymbol, error)
}

// WorkspaceSymbolInput is the input schema for lsp_workspace_symbol.
type WorkspaceSymbolInput struct {
	Query    string `json:"query"              jsonschema:"workspace symbol query"`
	Language string `json:"language,omitempty" jsonschema:"language id; defaults to go"`
}

// WorkspaceSymbolOutput is the output schema for lsp_workspace_symbol.
type WorkspaceSymbolOutput struct {
	Symbols []WorkspaceSymbolItem `json:"symbols"`
}

// WorkspaceSymbolItem is one workspace symbol result.
type WorkspaceSymbolItem struct {
	Name          string               `json:"name"`
	Kind          string               `json:"kind"`
	ContainerName string               `json:"containerName,omitempty"`
	URI           string               `json:"uri"`
	Range         *DefinitionRangeItem `json:"range,omitempty"`
}

func workspaceSymbolHandler(looker workspaceSymbolLooker) mcp.ToolHandlerFor[WorkspaceSymbolInput, WorkspaceSymbolOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in WorkspaceSymbolInput) (*mcp.CallToolResult, WorkspaceSymbolOutput, error) {
		lang := defaultedLanguage(in.Language)
		symbols, err := looker.Lookup(ctx, lang, in.Query)
		if err != nil {
			return nil, WorkspaceSymbolOutput{}, err
		}
		return nil, WorkspaceSymbolOutput{Symbols: toWorkspaceSymbolItems(symbols)}, nil
	}
}

func toWorkspaceSymbolItems(symbols []lsp.WorkspaceSymbol) []WorkspaceSymbolItem {
	items := make([]WorkspaceSymbolItem, 0, len(symbols))
	for _, symbol := range symbols {
		item := WorkspaceSymbolItem{Name: symbol.Name, Kind: symbol.Kind, ContainerName: symbol.ContainerName, URI: symbol.URI}
		if symbol.Range != nil {
			rng := toNavigationRangeItem(*symbol.Range)
			item.Range = &rng
		}
		items = append(items, item)
	}
	return items
}
