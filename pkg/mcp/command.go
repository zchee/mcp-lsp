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

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

type commandExecutor interface {
	Execute(ctx context.Context, lang, command string, args []protocol.LSPAny, applyEdits bool) (protocol.LSPAny, error)
}

// CommandItem is a compact command shape.
type CommandItem struct {
	Title   string `json:"title"`
	Tooltip string `json:"tooltip,omitempty"`
	Command string `json:"command"`
}

// ExecuteCommandInput is the input schema for lsp_execute_command.
type ExecuteCommandInput struct {
	Command    string            `json:"command"              jsonschema:"server-advertised command id"`
	Arguments  []protocol.LSPAny `json:"arguments,omitempty"  jsonschema:"optional raw JSON command arguments"`
	ApplyEdits bool              `json:"applyEdits,omitempty" jsonschema:"allow server-initiated workspace/applyEdit during command execution"`
	Language   string            `json:"language,omitempty"   jsonschema:"language id; defaults to go"`
}

// ExecuteCommandOutput is the output schema for lsp_execute_command.
type ExecuteCommandOutput struct {
	Result protocol.LSPAny `json:"result,omitempty"`
}

func executeCommandHandler(executor commandExecutor) mcp.ToolHandlerFor[ExecuteCommandInput, ExecuteCommandOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ExecuteCommandInput) (*mcp.CallToolResult, ExecuteCommandOutput, error) {
		if in.Command == "" {
			return nil, ExecuteCommandOutput{}, fmt.Errorf("command is required")
		}
		result, err := executor.Execute(ctx, defaultedLanguage(in.Language), in.Command, in.Arguments, in.ApplyEdits)
		if err != nil {
			return nil, ExecuteCommandOutput{}, err
		}
		return nil, ExecuteCommandOutput{Result: result}, nil
	}
}

func toCommandItem(command *lsp.Command) *CommandItem {
	if command == nil {
		return nil
	}
	return &CommandItem{Title: command.Title, Tooltip: command.Tooltip, Command: command.Command}
}
