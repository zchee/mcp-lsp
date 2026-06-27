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

type codeActionLooker interface {
	Lookup(ctx context.Context, lang, absPath, text string, rng protocol.Range, only []protocol.CodeActionKind, resolve bool) ([]lsp.CodeAction, error)
}

// CodeActionInput is the input schema for lsp_code_action.
type CodeActionInput struct {
	File        string   `json:"file"               jsonschema:"absolute or workspace-relative path to the file"`
	StartLine   int      `json:"startLine"          jsonschema:"one-based range start line"`
	StartColumn int      `json:"startColumn"        jsonschema:"one-based range start column"`
	EndLine     int      `json:"endLine"            jsonschema:"one-based range end line"`
	EndColumn   int      `json:"endColumn"          jsonschema:"one-based range end column"`
	Only        []string `json:"only,omitempty"     jsonschema:"optional code action kinds to request"`
	Resolve     bool     `json:"resolve,omitempty"  jsonschema:"resolve actions when the server supports it"`
	Language    string   `json:"language,omitempty" jsonschema:"language id of the file; defaults to go"`
}

// CodeActionOutput is the output schema for lsp_code_action.
type CodeActionOutput struct {
	File    string           `json:"file"`
	URI     string           `json:"uri"`
	Actions []CodeActionItem `json:"actions"`
}

// CodeActionItem is one code action preview.
type CodeActionItem struct {
	Title          string                  `json:"title"`
	Kind           string                  `json:"kind,omitempty"`
	IsPreferred    *bool                   `json:"isPreferred,omitempty"`
	DisabledReason string                  `json:"disabledReason,omitempty"`
	Edit           *protocol.WorkspaceEdit `json:"edit,omitempty"`
	Command        *CommandItem            `json:"command,omitempty"`
}

func codeActionHandler(looker codeActionLooker, workspaceRoot string) mcp.ToolHandlerFor[CodeActionInput, CodeActionOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in CodeActionInput) (*mcp.CallToolResult, CodeActionOutput, error) {
		rng, err := inputRange(in.StartLine, in.StartColumn, in.EndLine, in.EndColumn)
		if err != nil {
			return nil, CodeActionOutput{}, err
		}
		absPath, text, lang, err := readFeatureFile(workspaceRoot, in.File, in.Language)
		if err != nil {
			return nil, CodeActionOutput{}, err
		}
		actions, err := looker.Lookup(ctx, lang, absPath, text, rng, codeActionKinds(in.Only), in.Resolve)
		if err != nil {
			return nil, CodeActionOutput{}, err
		}
		items, err := toCodeActionItems(actions)
		if err != nil {
			return nil, CodeActionOutput{}, err
		}
		return nil, CodeActionOutput{File: absPath, URI: string(uri.File(absPath)), Actions: items}, nil
	}
}

func codeActionKinds(kinds []string) []protocol.CodeActionKind {
	out := make([]protocol.CodeActionKind, 0, len(kinds))
	for _, kind := range kinds {
		out = append(out, protocol.CodeActionKind(kind))
	}
	return out
}

func toCodeActionItems(actions []lsp.CodeAction) ([]CodeActionItem, error) {
	items := make([]CodeActionItem, 0, len(actions))
	for _, action := range actions {
		item := CodeActionItem{Title: action.Title, Kind: action.Kind, IsPreferred: action.IsPreferred, DisabledReason: action.DisabledReason, Command: toCommandItem(action.Command)}
		if action.Edit != nil {
			wire, err := lsp.WorkspaceEditToProtocol(*action.Edit)
			if err != nil {
				return nil, err
			}
			item.Edit = &wire
		}
		items = append(items, item)
	}
	return items, nil
}
