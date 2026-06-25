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
	"fmt"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

// implLooker is the narrow dependency the implementation handler needs from
// the LSP layer. It lets tests substitute a fake without spawning a language
// server.
type implLooker interface {
	Lookup(ctx context.Context, lang, absPath, text string, pos protocol.Position) ([]lsp.ImplementationLocation, error)
}

// ImplementationInput is the input schema for the lsp_implementation tool.
// Line and column are one-based for the agent.
type ImplementationInput struct {
	File     string `json:"file"               jsonschema:"absolute or workspace-relative path to the file to query"`
	Line     int    `json:"line"               jsonschema:"one-based line containing the symbol reference"`
	Column   int    `json:"column"             jsonschema:"one-based column containing the symbol reference"`
	Language string `json:"language,omitempty" jsonschema:"language id of the file; defaults to go"`
}

// ImplementationRangeItem is a one-based range returned by the
// lsp_implementation tool.
type ImplementationRangeItem = DefinitionRangeItem

// ImplementationItem is one implementation target returned by the language
// server.
type ImplementationItem struct {
	TargetURI            string                   `json:"targetUri"`
	TargetRange          ImplementationRangeItem  `json:"targetRange"`
	TargetSelectionRange ImplementationRangeItem  `json:"targetSelectionRange"`
	OriginSelectionRange *ImplementationRangeItem `json:"originSelectionRange,omitempty"`
}

// ImplementationOutput is the output schema for the lsp_implementation tool.
type ImplementationOutput struct {
	File            string               `json:"file"`
	URI             string               `json:"uri"`
	Implementations []ImplementationItem `json:"implementations"`
}

// implementationHandler returns the tool handler bound to looker. The handler
// validates input, reads the file, looks up implementations, and converts
// one-based agent positions at the MCP boundary.
func implementationHandler(looker implLooker, workspaceRoot string) mcp.ToolHandlerFor[ImplementationInput, ImplementationOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ImplementationInput) (*mcp.CallToolResult, ImplementationOutput, error) {
		if in.File == "" {
			return nil, ImplementationOutput{}, fmt.Errorf("file is required")
		}
		pos, err := navigationInputPosition(in.Line, in.Column)
		if err != nil {
			return nil, ImplementationOutput{}, err
		}

		absPath, err := resolveFilePath(workspaceRoot, in.File)
		if err != nil {
			return nil, ImplementationOutput{}, fmt.Errorf("resolve file path %q: %w", in.File, err)
		}

		lang := in.Language
		if lang == "" {
			lang = defaultLanguage
		}

		text, err := os.ReadFile(absPath)
		if err != nil {
			return nil, ImplementationOutput{}, fmt.Errorf("read file %q: %w", absPath, err)
		}

		implementations, err := looker.Lookup(ctx, lang, absPath, string(text), pos)
		if err != nil {
			return nil, ImplementationOutput{}, err
		}
		return nil, ImplementationOutput{
			File:            absPath,
			URI:             string(uri.File(absPath)),
			Implementations: toImplementationItems(implementations),
		}, nil
	}
}

// toImplementationItems converts zero-based [lsp.ImplementationLocation]
// values into one-based tool items.
func toImplementationItems(implementations []lsp.ImplementationLocation) []ImplementationItem {
	items := make([]ImplementationItem, 0, len(implementations))
	for _, implementation := range implementations {
		item := ImplementationItem{
			TargetURI:            implementation.TargetURI,
			TargetRange:          toNavigationRangeItem(implementation.TargetRange),
			TargetSelectionRange: toNavigationRangeItem(implementation.TargetSelectionRange),
		}
		if implementation.OriginSelectionRange != nil {
			origin := toNavigationRangeItem(*implementation.OriginSelectionRange)
			item.OriginSelectionRange = &origin
		}
		items = append(items, item)
	}
	return items
}
