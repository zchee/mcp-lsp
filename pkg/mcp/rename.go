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

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

type renamer interface {
	Preview(ctx context.Context, lang, absPath, text string, pos protocol.Position, newName string) (lsp.WorkspaceEdit, error)
}

// RenameInput is the input schema for lsp_rename.
type RenameInput struct {
	File     string `json:"file"               jsonschema:"absolute or workspace-relative path to the file to rename in"`
	Line     int    `json:"line"               jsonschema:"one-based line containing the symbol"`
	Column   int    `json:"column"             jsonschema:"one-based column containing the symbol"`
	NewName  string `json:"newName"            jsonschema:"new symbol name"`
	Language string `json:"language,omitempty" jsonschema:"language id of the file; inferred from file when omitted"`
}

func renameHandler(renamer renamer, workspaceRoot string, resolver languageResolver) mcp.ToolHandlerFor[RenameInput, WorkspaceEditPreviewOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in RenameInput) (*mcp.CallToolResult, WorkspaceEditPreviewOutput, error) {
		if in.NewName == "" {
			return nil, WorkspaceEditPreviewOutput{}, fmt.Errorf("newName is required")
		}
		pos, err := navigationInputPosition(in.Line, in.Column)
		if err != nil {
			return nil, WorkspaceEditPreviewOutput{}, err
		}
		absPath, text, lang, err := readInputFile(workspaceRoot, in.File, in.Language, resolver)
		if err != nil {
			return nil, WorkspaceEditPreviewOutput{}, err
		}
		edit, err := renamer.Preview(ctx, lang, absPath, text, pos, in.NewName)
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
