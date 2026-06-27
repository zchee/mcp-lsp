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

type formatter interface {
	Format(ctx context.Context, lang, absPath, text string, options protocol.FormattingOptions) (lsp.WorkspaceEdit, error)
	RangeFormat(ctx context.Context, lang, absPath, text string, rng protocol.Range, options protocol.FormattingOptions) (lsp.WorkspaceEdit, error)
}

// FormattingInput is the input schema for lsp_formatting.
type FormattingInput struct {
	File         string `json:"file"                   jsonschema:"absolute or workspace-relative path to the file to format"`
	Language     string `json:"language,omitempty"     jsonschema:"language id of the file; defaults to go"`
	TabSize      uint32 `json:"tabSize,omitempty"      jsonschema:"tab size; defaults to 4"`
	InsertSpaces *bool  `json:"insertSpaces,omitempty" jsonschema:"whether to insert spaces; defaults to true"`
}

// RangeFormattingInput is the input schema for lsp_range_formatting.
type RangeFormattingInput struct {
	FormattingInput
	StartLine   int `json:"startLine"   jsonschema:"one-based range start line"`
	StartColumn int `json:"startColumn" jsonschema:"one-based range start column"`
	EndLine     int `json:"endLine"     jsonschema:"one-based range end line"`
	EndColumn   int `json:"endColumn"   jsonschema:"one-based range end column"`
}

func formattingHandler(formatter formatter, workspaceRoot string) mcp.ToolHandlerFor[FormattingInput, WorkspaceEditPreviewOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in FormattingInput) (*mcp.CallToolResult, WorkspaceEditPreviewOutput, error) {
		absPath, text, lang, err := readFeatureFile(workspaceRoot, in.File, in.Language)
		if err != nil {
			return nil, WorkspaceEditPreviewOutput{}, err
		}
		edit, err := formatter.Format(ctx, lang, absPath, text, formattingOptions(in.TabSize, in.InsertSpaces))
		if err != nil {
			return nil, WorkspaceEditPreviewOutput{}, err
		}
		wire, err := lsp.WorkspaceEditToProtocol(edit)
		if err != nil {
			return nil, WorkspaceEditPreviewOutput{}, err
		}
		return nil, WorkspaceEditPreviewOutput{File: absPath, URI: string(uri.File(absPath)), Edit: wire}, nil
	}
}

func rangeFormattingHandler(formatter formatter, workspaceRoot string) mcp.ToolHandlerFor[RangeFormattingInput, WorkspaceEditPreviewOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in RangeFormattingInput) (*mcp.CallToolResult, WorkspaceEditPreviewOutput, error) {
		rng, err := inputRange(in.StartLine, in.StartColumn, in.EndLine, in.EndColumn)
		if err != nil {
			return nil, WorkspaceEditPreviewOutput{}, err
		}
		absPath, text, lang, err := readFeatureFile(workspaceRoot, in.File, in.Language)
		if err != nil {
			return nil, WorkspaceEditPreviewOutput{}, err
		}
		edit, err := formatter.RangeFormat(ctx, lang, absPath, text, rng, formattingOptions(in.TabSize, in.InsertSpaces))
		if err != nil {
			return nil, WorkspaceEditPreviewOutput{}, err
		}
		wire, err := lsp.WorkspaceEditToProtocol(edit)
		if err != nil {
			return nil, WorkspaceEditPreviewOutput{}, err
		}
		return nil, WorkspaceEditPreviewOutput{File: absPath, URI: string(uri.File(absPath)), Edit: wire}, nil
	}
}

func formattingOptions(tabSize uint32, insertSpaces *bool) protocol.FormattingOptions {
	if tabSize == 0 {
		tabSize = 4
	}
	spaces := true
	if insertSpaces != nil {
		spaces = *insertSpaces
	}
	return protocol.FormattingOptions{TabSize: tabSize, InsertSpaces: spaces}
}
