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
	"go.lsp.dev/uri"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

type codeLensLooker interface {
	Lookup(ctx context.Context, lang, absPath, text string, resolve bool) ([]lsp.CodeLens, error)
}

// CodeLensInput is the input schema for lsp_code_lens.
type CodeLensInput struct {
	File     string `json:"file"               jsonschema:"absolute or workspace-relative path to the file"`
	Resolve  bool   `json:"resolve,omitempty"  jsonschema:"resolve lenses when the server supports it"`
	Language string `json:"language,omitempty" jsonschema:"language id of the file; inferred from file when omitted"`
}

// CodeLensOutput is the output schema for lsp_code_lens.
type CodeLensOutput struct {
	File   string         `json:"file"`
	URI    string         `json:"uri"`
	Lenses []CodeLensItem `json:"lenses"`
}

// CodeLensItem is one code lens result.
type CodeLensItem struct {
	Range   DefinitionRangeItem `json:"range"`
	Command *CommandItem        `json:"command,omitempty"`
}

func codeLensHandler(looker codeLensLooker, workspaceRoot string, resolver languageResolver) mcp.ToolHandlerFor[CodeLensInput, CodeLensOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in CodeLensInput) (*mcp.CallToolResult, CodeLensOutput, error) {
		absPath, text, lang, err := readInputFile(workspaceRoot, in.File, in.Language, resolver)
		if err != nil {
			return nil, CodeLensOutput{}, err
		}
		lenses, err := looker.Lookup(ctx, lang, absPath, text, in.Resolve)
		if err != nil {
			return nil, CodeLensOutput{}, err
		}
		return nil, CodeLensOutput{File: absPath, URI: string(uri.File(absPath)), Lenses: toCodeLensItems(lenses)}, nil
	}
}

func toCodeLensItems(lenses []lsp.CodeLens) []CodeLensItem {
	items := make([]CodeLensItem, 0, len(lenses))
	for _, lens := range lenses {
		items = append(
			items, CodeLensItem{
				Range:   toNavigationRangeItem(lens.Range),
				Command: toCommandItem(lens.Command),
			},
		)
	}
	return items
}
