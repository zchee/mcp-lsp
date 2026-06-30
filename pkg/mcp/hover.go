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
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

type hoverLooker interface {
	Lookup(ctx context.Context, lang, absPath, text string, pos protocol.Position) (*lsp.HoverResult, error)
}

// HoverInput is the input schema for lsp_hover.
type HoverInput struct {
	File     string `json:"file"               jsonschema:"absolute or workspace-relative path to the file to query"`
	Line     int    `json:"line"               jsonschema:"one-based line containing the hover position"`
	Column   int    `json:"column"             jsonschema:"one-based column containing the hover position"`
	Language string `json:"language,omitempty" jsonschema:"language id of the file; defaults to go"`
}

// HoverOutput is the output schema for lsp_hover.
type HoverOutput struct {
	File  string     `json:"file"`
	URI   string     `json:"uri"`
	Hover *HoverItem `json:"hover,omitempty"`
}

// HoverItem is a hover response.
type HoverItem struct {
	Kind  string               `json:"kind"`
	Value string               `json:"value"`
	Range *DefinitionRangeItem `json:"range,omitempty"`
}

func hoverHandler(looker hoverLooker, workspaceRoot string, defaultLang ...string) mcp.ToolHandlerFor[HoverInput, HoverOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in HoverInput) (*mcp.CallToolResult, HoverOutput, error) {
		pos, err := navigationInputPosition(in.Line, in.Column)
		if err != nil {
			return nil, HoverOutput{}, err
		}
		absPath, text, lang, err := readInputFile(workspaceRoot, in.File, in.Language, defaultLang...)
		if err != nil {
			return nil, HoverOutput{}, err
		}
		hover, err := looker.Lookup(ctx, lang, absPath, text, pos)
		if err != nil {
			return nil, HoverOutput{}, err
		}
		return nil, HoverOutput{File: absPath, URI: string(uri.File(absPath)), Hover: toHoverItem(hover)}, nil
	}
}

func toHoverItem(hover *lsp.HoverResult) *HoverItem {
	if hover == nil {
		return nil
	}
	item := &HoverItem{Kind: hover.Kind, Value: hover.Value}
	if hover.Range != nil {
		rng := toNavigationRangeItem(*hover.Range)
		item.Range = &rng
	}
	return item
}
