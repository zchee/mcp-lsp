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

type hoverLooker interface {
	Lookup(ctx context.Context, lang, absPath, text string, pos protocol.Position) (*lsp.HoverResult, error)
}

type workspaceSymbolLooker interface {
	Lookup(ctx context.Context, lang, query string) ([]lsp.WorkspaceSymbol, error)
}

type formatter interface {
	Format(ctx context.Context, lang, absPath, text string, options protocol.FormattingOptions) (lsp.WorkspaceEdit, error)
	RangeFormat(ctx context.Context, lang, absPath, text string, rng protocol.Range, options protocol.FormattingOptions) (lsp.WorkspaceEdit, error)
}

type renamer interface {
	Preview(ctx context.Context, lang, absPath, text string, pos protocol.Position, newName string) (lsp.WorkspaceEdit, error)
}

type codeActionLooker interface {
	Lookup(ctx context.Context, lang, absPath, text string, rng protocol.Range, only []protocol.CodeActionKind, resolve bool) ([]lsp.CodeAction, error)
}

type codeLensLooker interface {
	Lookup(ctx context.Context, lang, absPath, text string, resolve bool) ([]lsp.CodeLens, error)
}

type commandExecutor interface {
	Execute(ctx context.Context, lang, command string, args []protocol.LSPAny, applyEdits bool) (protocol.LSPAny, error)
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

func hoverHandler(looker hoverLooker, workspaceRoot string) mcp.ToolHandlerFor[HoverInput, HoverOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in HoverInput) (*mcp.CallToolResult, HoverOutput, error) {
		pos, err := navigationInputPosition(in.Line, in.Column)
		if err != nil {
			return nil, HoverOutput{}, err
		}
		absPath, text, lang, err := readFeatureFile(workspaceRoot, in.File, in.Language)
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

// WorkspaceEditPreviewOutput wraps a non-applied LSP workspace edit preview.
type WorkspaceEditPreviewOutput struct {
	File string                 `json:"file,omitempty"`
	URI  string                 `json:"uri,omitempty"`
	Edit protocol.WorkspaceEdit `json:"edit"`
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
		absPath, text, lang, err := readFeatureFile(workspaceRoot, in.File, in.Language)
		if err != nil {
			return nil, WorkspaceEditPreviewOutput{}, err
		}
		rng, err := inputRange(in.StartLine, in.StartColumn, in.EndLine, in.EndColumn)
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

// RenameInput is the input schema for lsp_rename.
type RenameInput struct {
	File     string `json:"file"               jsonschema:"absolute or workspace-relative path to the file to rename in"`
	Line     int    `json:"line"               jsonschema:"one-based line containing the symbol"`
	Column   int    `json:"column"             jsonschema:"one-based column containing the symbol"`
	NewName  string `json:"newName"            jsonschema:"new symbol name"`
	Language string `json:"language,omitempty" jsonschema:"language id of the file; defaults to go"`
}

func renameHandler(renamer renamer, workspaceRoot string) mcp.ToolHandlerFor[RenameInput, WorkspaceEditPreviewOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in RenameInput) (*mcp.CallToolResult, WorkspaceEditPreviewOutput, error) {
		if in.NewName == "" {
			return nil, WorkspaceEditPreviewOutput{}, fmt.Errorf("newName is required")
		}
		pos, err := navigationInputPosition(in.Line, in.Column)
		if err != nil {
			return nil, WorkspaceEditPreviewOutput{}, err
		}
		absPath, text, lang, err := readFeatureFile(workspaceRoot, in.File, in.Language)
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

// CommandItem is a compact command shape.
type CommandItem struct {
	Title   string `json:"title"`
	Tooltip string `json:"tooltip,omitempty"`
	Command string `json:"command"`
}

func codeActionHandler(looker codeActionLooker, workspaceRoot string) mcp.ToolHandlerFor[CodeActionInput, CodeActionOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in CodeActionInput) (*mcp.CallToolResult, CodeActionOutput, error) {
		absPath, text, lang, err := readFeatureFile(workspaceRoot, in.File, in.Language)
		if err != nil {
			return nil, CodeActionOutput{}, err
		}
		rng, err := inputRange(in.StartLine, in.StartColumn, in.EndLine, in.EndColumn)
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

// CodeLensInput is the input schema for lsp_code_lens.
type CodeLensInput struct {
	File     string `json:"file"               jsonschema:"absolute or workspace-relative path to the file"`
	Resolve  bool   `json:"resolve,omitempty"  jsonschema:"resolve lenses when the server supports it"`
	Language string `json:"language,omitempty" jsonschema:"language id of the file; defaults to go"`
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

func codeLensHandler(looker codeLensLooker, workspaceRoot string) mcp.ToolHandlerFor[CodeLensInput, CodeLensOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in CodeLensInput) (*mcp.CallToolResult, CodeLensOutput, error) {
		absPath, text, lang, err := readFeatureFile(workspaceRoot, in.File, in.Language)
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

func readFeatureFile(workspaceRoot, file, lang string) (absPath, text, resolvedLang string, err error) {
	if file == "" {
		return "", "", "", fmt.Errorf("file is required")
	}
	absPath, err = resolveFilePath(workspaceRoot, file)
	if err != nil {
		return "", "", "", fmt.Errorf("resolve file path %q: %w", file, err)
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		return "", "", "", fmt.Errorf("read file %q: %w", absPath, err)
	}
	return absPath, string(content), defaultedLanguage(lang), nil
}

func defaultedLanguage(lang string) string {
	if lang == "" {
		return defaultLanguage
	}
	return lang
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

func inputRange(startLine, startColumn, endLine, endColumn int) (protocol.Range, error) {
	start, err := navigationInputPosition(startLine, startColumn)
	if err != nil {
		return protocol.Range{}, err
	}
	end, err := navigationInputPosition(endLine, endColumn)
	if err != nil {
		return protocol.Range{}, err
	}
	return protocol.Range{Start: start, End: end}, nil
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

func toCommandItem(command *lsp.Command) *CommandItem {
	if command == nil {
		return nil
	}
	return &CommandItem{Title: command.Title, Tooltip: command.Tooltip, Command: command.Command}
}

func toCodeLensItems(lenses []lsp.CodeLens) []CodeLensItem {
	items := make([]CodeLensItem, 0, len(lenses))
	for _, lens := range lenses {
		items = append(items, CodeLensItem{Range: toNavigationRangeItem(lens.Range), Command: toCommandItem(lens.Command)})
	}
	return items
}
