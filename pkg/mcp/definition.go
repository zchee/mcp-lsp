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

const maxProtocolPositionInput = int64(1) << 32

// defLooker is the narrow dependency the definition handler needs from the LSP
// layer. It lets tests substitute a fake without spawning a language server.
type defLooker interface {
	Lookup(ctx context.Context, lang, absPath, text string, pos protocol.Position) ([]lsp.DefinitionLocation, error)
}

// DefinitionInput is the input schema for the lsp_definition tool. Line and
// column are one-based for the agent.
type DefinitionInput struct {
	File     string `json:"file"               jsonschema:"absolute or workspace-relative path to the file to query"`
	Line     int    `json:"line"               jsonschema:"one-based line containing the symbol reference"`
	Column   int    `json:"column"             jsonschema:"one-based column containing the symbol reference"`
	Language string `json:"language,omitempty" jsonschema:"language id of the file; defaults to go"`
}

// DefinitionRangeItem is a one-based range returned by the lsp_definition tool.
type DefinitionRangeItem struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn"`
	EndLine     int `json:"endLine"`
	EndColumn   int `json:"endColumn"`
}

// DefinitionItem is one definition target returned by the language server.
type DefinitionItem struct {
	TargetURI            string               `json:"targetUri"`
	TargetRange          DefinitionRangeItem  `json:"targetRange"`
	TargetSelectionRange DefinitionRangeItem  `json:"targetSelectionRange"`
	OriginSelectionRange *DefinitionRangeItem `json:"originSelectionRange,omitempty"`
}

// DefinitionOutput is the output schema for the lsp_definition tool.
type DefinitionOutput struct {
	File        string           `json:"file"`
	URI         string           `json:"uri"`
	Definitions []DefinitionItem `json:"definitions"`
}

// definitionHandler returns the tool handler bound to looker. The handler
// validates input, reads the file, looks up definitions, and converts one-based
// agent positions at the MCP boundary.
func definitionHandler(looker defLooker, workspaceRoot string) mcp.ToolHandlerFor[DefinitionInput, DefinitionOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in DefinitionInput) (*mcp.CallToolResult, DefinitionOutput, error) {
		if in.File == "" {
			return nil, DefinitionOutput{}, fmt.Errorf("file is required")
		}
		pos, err := definitionInputPosition(in.Line, in.Column)
		if err != nil {
			return nil, DefinitionOutput{}, err
		}

		absPath, err := resolveFilePath(workspaceRoot, in.File)
		if err != nil {
			return nil, DefinitionOutput{}, fmt.Errorf("resolve file path %q: %w", in.File, err)
		}

		lang := in.Language
		if lang == "" {
			lang = defaultLanguage
		}

		text, err := os.ReadFile(absPath)
		if err != nil {
			return nil, DefinitionOutput{}, fmt.Errorf("read file %q: %w", absPath, err)
		}

		defs, err := looker.Lookup(ctx, lang, absPath, string(text), pos)
		if err != nil {
			return nil, DefinitionOutput{}, err
		}

		return nil, DefinitionOutput{
			File:        absPath,
			URI:         string(uri.File(absPath)),
			Definitions: toDefinitionItems(defs),
		}, nil
	}
}

func definitionInputPosition(line, column int) (protocol.Position, error) {
	protocolLine, err := definitionInputCoordinate("line", line)
	if err != nil {
		return protocol.Position{}, err
	}
	protocolColumn, err := definitionInputCoordinate("column", column)
	if err != nil {
		return protocol.Position{}, err
	}

	return protocol.Position{Line: protocolLine, Character: protocolColumn}, nil
}

func definitionInputCoordinate(name string, value int) (uint32, error) {
	if value <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", name)
	}
	if int64(value) > maxProtocolPositionInput {
		return 0, fmt.Errorf("%s must be less than or equal to %d", name, maxProtocolPositionInput)
	}

	return uint32(value - 1), nil
}

// toDefinitionItems converts zero-based [lsp.DefinitionLocation] values into
// one-based tool items.
func toDefinitionItems(defs []lsp.DefinitionLocation) []DefinitionItem {
	items := make([]DefinitionItem, 0, len(defs))
	for _, def := range defs {
		item := DefinitionItem{
			TargetURI:            def.TargetURI,
			TargetRange:          toDefinitionRangeItem(def.TargetRange),
			TargetSelectionRange: toDefinitionRangeItem(def.TargetSelectionRange),
		}
		if def.OriginSelectionRange != nil {
			origin := toDefinitionRangeItem(*def.OriginSelectionRange)
			item.OriginSelectionRange = &origin
		}
		items = append(items, item)
	}

	return items
}

func toDefinitionRangeItem(rng lsp.DefinitionRange) DefinitionRangeItem {
	return DefinitionRangeItem{
		StartLine:   rng.StartLine + 1,
		StartColumn: rng.StartColumn + 1,
		EndLine:     rng.EndLine + 1,
		EndColumn:   rng.EndColumn + 1,
	}
}
