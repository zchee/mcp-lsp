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

package lsp

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// Hover is the textDocument/hover feature bound to a [Manager].
type Hover struct {
	mgr     *Manager
	timeout time.Duration
}

// Hover returns the textDocument/hover feature for this manager.
func (m *Manager) Hover() *Hover { return &Hover{mgr: m, timeout: defaultTimeout} }

// HoverResult is a compact hover response with zero-based range coordinates.
type HoverResult struct {
	Kind  string
	Value string
	Range *NavigationRange
}

// Lookup opens absPath, synchronizes its text, and returns hover content for pos.
func (h *Hover) Lookup(ctx context.Context, lang, absPath, text string, pos protocol.Position) (*HoverResult, error) {
	ctx, cancel := featureTimeout(ctx, h.timeout)
	defer cancel()

	sess, u, err := h.mgr.syncSessionForFile(ctx, lang, absPath, text)
	if err != nil {
		return nil, err
	}
	if !sess.capabilities.hover {
		return nil, fmt.Errorf("hover request is not supported by language server")
	}

	result, err := sess.server.Hover(ctx, &protocol.HoverParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}, Position: pos}})
	if err != nil {
		return nil, fmt.Errorf("hover request: %w", err)
	}
	return flattenHover(result), nil
}

// WorkspaceSymbols is the workspace/symbol feature bound to a [Manager].
type WorkspaceSymbols struct {
	mgr     *Manager
	timeout time.Duration
}

// WorkspaceSymbols returns the workspace/symbol feature for this manager.
func (m *Manager) WorkspaceSymbols() *WorkspaceSymbols {
	return &WorkspaceSymbols{mgr: m, timeout: defaultTimeout}
}

// WorkspaceSymbol is a compact workspace symbol response with zero-based ranges.
type WorkspaceSymbol struct {
	Name          string
	Kind          string
	ContainerName string
	URI           string
	Range         *NavigationRange
}

// Lookup returns workspace symbols for query.
func (w *WorkspaceSymbols) Lookup(ctx context.Context, lang, query string) ([]WorkspaceSymbol, error) {
	ctx, cancel := featureTimeout(ctx, w.timeout)
	defer cancel()

	sess, err := w.mgr.session(ctx, lang)
	if err != nil {
		return nil, err
	}
	if !sess.capabilities.workspaceSymbol {
		return nil, fmt.Errorf("workspace/symbol request is not supported by language server")
	}
	result, err := sess.server.Symbols(ctx, &protocol.WorkspaceSymbolParams{Query: query})
	if err != nil {
		return nil, fmt.Errorf("workspace/symbol request: %w", err)
	}
	return flattenWorkspaceSymbols(result), nil
}

// Formatting is the textDocument formatting feature bound to a [Manager].
type Formatting struct {
	mgr     *Manager
	timeout time.Duration
}

// Formatting returns the formatting feature for this manager.
func (m *Manager) Formatting() *Formatting { return &Formatting{mgr: m, timeout: defaultTimeout} }

// Format returns a workspace edit preview for textDocument/formatting.
func (f *Formatting) Format(ctx context.Context, lang, absPath, text string, options protocol.FormattingOptions) (WorkspaceEdit, error) {
	ctx, cancel := featureTimeout(ctx, f.timeout)
	defer cancel()

	sess, u, err := f.mgr.syncSessionForFile(ctx, lang, absPath, text)
	if err != nil {
		return WorkspaceEdit{}, err
	}
	if !sess.capabilities.formatting {
		return WorkspaceEdit{}, fmt.Errorf("formatting request is not supported by language server")
	}
	edits, err := sess.server.Formatting(ctx, &protocol.DocumentFormattingParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}, Options: options})
	if err != nil {
		return WorkspaceEdit{}, fmt.Errorf("formatting request: %w", err)
	}
	return workspaceEditForTextEdits(u, edits), nil
}

// RangeFormat returns a workspace edit preview for textDocument/rangeFormatting.
func (f *Formatting) RangeFormat(ctx context.Context, lang, absPath, text string, rng protocol.Range, options protocol.FormattingOptions) (WorkspaceEdit, error) {
	ctx, cancel := featureTimeout(ctx, f.timeout)
	defer cancel()

	sess, u, err := f.mgr.syncSessionForFile(ctx, lang, absPath, text)
	if err != nil {
		return WorkspaceEdit{}, err
	}
	if !sess.capabilities.rangeFormatting {
		return WorkspaceEdit{}, fmt.Errorf("range formatting request is not supported by language server")
	}
	edits, err := sess.server.RangeFormatting(ctx, &protocol.DocumentRangeFormattingParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}, Range: rng, Options: options})
	if err != nil {
		return WorkspaceEdit{}, fmt.Errorf("range formatting request: %w", err)
	}
	return workspaceEditForTextEdits(u, edits), nil
}

// Rename is the textDocument/rename feature bound to a [Manager].
type Rename struct {
	mgr     *Manager
	timeout time.Duration
}

// Rename returns the rename feature for this manager.
func (m *Manager) Rename() *Rename { return &Rename{mgr: m, timeout: defaultTimeout} }

// Preview returns a workspace edit preview for textDocument/rename.
func (r *Rename) Preview(ctx context.Context, lang, absPath, text string, pos protocol.Position, newName string) (WorkspaceEdit, error) {
	ctx, cancel := featureTimeout(ctx, r.timeout)
	defer cancel()

	sess, u, err := r.mgr.syncSessionForFile(ctx, lang, absPath, text)
	if err != nil {
		return WorkspaceEdit{}, err
	}
	if !sess.capabilities.rename {
		return WorkspaceEdit{}, fmt.Errorf("rename request is not supported by language server")
	}
	edit, err := sess.server.Rename(ctx, &protocol.RenameParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}, Position: pos}, NewName: newName})
	if err != nil {
		return WorkspaceEdit{}, fmt.Errorf("rename request: %w", err)
	}
	if edit == nil {
		return WorkspaceEdit{}, nil
	}
	return WorkspaceEditFromProtocol(*edit)
}

// CodeActions is the textDocument/codeAction feature bound to a [Manager].
type CodeActions struct {
	mgr     *Manager
	timeout time.Duration
}

// CodeActions returns the code action feature for this manager.
func (m *Manager) CodeActions() *CodeActions { return &CodeActions{mgr: m, timeout: defaultTimeout} }

// Command is a compact command DTO.
type Command struct {
	Title   string
	Tooltip string
	Command string
}

// CodeAction is a compact code action DTO.
type CodeAction struct {
	Title          string
	Kind           string
	IsPreferred    *bool
	DisabledReason string
	Edit           *WorkspaceEdit
	Command        *Command
}

// Lookup returns code actions for rng, optionally resolving actions when supported.
func (c *CodeActions) Lookup(ctx context.Context, lang, absPath, text string, rng protocol.Range, only []protocol.CodeActionKind, resolve bool) ([]CodeAction, error) {
	ctx, cancel := featureTimeout(ctx, c.timeout)
	defer cancel()

	sess, u, err := c.mgr.syncSessionForFile(ctx, lang, absPath, text)
	if err != nil {
		return nil, err
	}
	if !sess.capabilities.codeAction {
		return nil, fmt.Errorf("code action request is not supported by language server")
	}
	raw, err := sess.server.CodeAction(ctx, &protocol.CodeActionParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}, Range: rng, Context: protocol.CodeActionContext{Diagnostics: []protocol.Diagnostic{}, Only: only}})
	if err != nil {
		return nil, fmt.Errorf("code action request: %w", err)
	}
	out := make([]CodeAction, 0, len(raw))
	for _, item := range raw {
		action, err := c.flattenCodeAction(ctx, sess, item, resolve)
		if err != nil {
			return nil, err
		}
		out = append(out, action)
	}
	return out, nil
}

func (c *CodeActions) flattenCodeAction(ctx context.Context, sess *serverSession, item protocol.CommandOrCodeAction, resolve bool) (CodeAction, error) {
	switch v := item.(type) {
	case *protocol.Command:
		return CodeAction{Title: v.Title, Command: flattenCommand(*v)}, nil
	case *protocol.CodeAction:
		if v == nil {
			return CodeAction{}, fmt.Errorf("nil code action")
		}
		if resolve && sess.capabilities.codeActionResolve {
			resolved, err := sess.server.CodeActionResolve(ctx, v)
			if err != nil {
				return CodeAction{}, fmt.Errorf("codeAction/resolve request: %w", err)
			}
			if resolved != nil {
				v = resolved
			}
		}
		return flattenCodeActionStruct(v)
	default:
		return CodeAction{}, fmt.Errorf("unsupported code action item %T", item)
	}
}

// CodeLenses is the textDocument/codeLens feature bound to a [Manager].
type CodeLenses struct {
	mgr     *Manager
	timeout time.Duration
}

// CodeLenses returns the code lens feature for this manager.
func (m *Manager) CodeLenses() *CodeLenses { return &CodeLenses{mgr: m, timeout: defaultTimeout} }

// CodeLens is a compact code lens DTO.
type CodeLens struct {
	Range   NavigationRange
	Command *Command
}

// Lookup returns code lenses, optionally resolving command fields when supported.
func (c *CodeLenses) Lookup(ctx context.Context, lang, absPath, text string, resolve bool) ([]CodeLens, error) {
	ctx, cancel := featureTimeout(ctx, c.timeout)
	defer cancel()

	sess, u, err := c.mgr.syncSessionForFile(ctx, lang, absPath, text)
	if err != nil {
		return nil, err
	}
	if !sess.capabilities.codeLens {
		return nil, fmt.Errorf("code lens request is not supported by language server")
	}
	raw, err := sess.server.CodeLens(ctx, &protocol.CodeLensParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}})
	if err != nil {
		return nil, fmt.Errorf("code lens request: %w", err)
	}
	out := make([]CodeLens, 0, len(raw))
	for i := range raw {
		lens := &raw[i]
		if resolve && sess.capabilities.codeLensResolve && lens.Command.Command == "" {
			resolved, err := sess.server.CodeLensResolve(ctx, lens)
			if err != nil {
				return nil, fmt.Errorf("codeLens/resolve request: %w", err)
			}
			if resolved != nil {
				lens = resolved
			}
		}
		item := CodeLens{Range: navigationRangeFromProtocol(lens.Range)}
		if lens.Command.Command != "" || lens.Command.Title != "" {
			item.Command = flattenCommand(lens.Command)
		}
		out = append(out, item)
	}
	return out, nil
}

// Commands is the workspace/executeCommand feature bound to a [Manager].
type Commands struct {
	mgr     *Manager
	timeout time.Duration
}

// Commands returns the execute-command feature for this manager.
func (m *Manager) Commands() *Commands { return &Commands{mgr: m, timeout: defaultTimeout} }

// Execute runs an advertised workspace command.
func (c *Commands) Execute(ctx context.Context, lang, command string, args []protocol.LSPAny, applyEdits bool) (protocol.LSPAny, error) {
	ctx, cancel := featureTimeout(ctx, c.timeout)
	defer cancel()

	sess, err := c.mgr.session(ctx, lang)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(sess.capabilities.executeCommands, command) {
		return nil, fmt.Errorf("execute command %q is not advertised by language server", command)
	}
	run := func() error { return nil }
	_ = run
	var result protocol.LSPAny
	exec := func() error {
		var err error
		result, err = sess.server.ExecuteCommand(ctx, &protocol.ExecuteCommandParams{Command: command, Arguments: args})
		if err != nil {
			return fmt.Errorf("execute command request: %w", err)
		}
		return nil
	}
	if applyEdits {
		if sess.client == nil {
			return nil, fmt.Errorf("execute command apply mode requires initialized client")
		}
		if err := sess.client.withApplyEditPolicy(WorkspaceEditApplyOptions{WorkspaceRoot: c.mgr.rootDir}, exec); err != nil {
			return nil, err
		}
		return result, nil
	}
	if err := exec(); err != nil {
		return nil, err
	}
	return result, nil
}

func (m *Manager) syncSessionForFile(ctx context.Context, lang, absPath, text string) (*serverSession, uri.URI, error) {
	sess, err := m.session(ctx, lang)
	if err != nil {
		return nil, "", err
	}
	cfg := m.cfg[lang]
	u := uri.File(absPath)
	if err := sess.syncTextDocument(ctx, u, cfg.LanguageID, text); err != nil {
		return nil, "", err
	}
	return sess, u, nil
}

func flattenHover(in *protocol.Hover) *HoverResult {
	if in == nil {
		return nil
	}
	out := &HoverResult{Kind: "plaintext", Value: hoverContentsText(in.Contents)}
	if markup, ok := in.Contents.(*protocol.MarkupContent); ok && markup != nil {
		out.Kind = string(markup.Kind)
		out.Value = markup.Value
	}
	if in.Range != nil {
		rng := navigationRangeFromProtocol(*in.Range)
		out.Range = &rng
	}
	return out
}

func hoverContentsText(contents protocol.HoverContents) string {
	switch v := contents.(type) {
	case nil:
		return ""
	case protocol.String:
		return string(v)
	case *protocol.MarkupContent:
		if v == nil {
			return ""
		}
		return v.Value
	case *protocol.MarkedStringWithLanguage:
		if v == nil {
			return ""
		}
		if v.Language == "" {
			return v.Value
		}
		return "```" + v.Language + "\n" + v.Value + "\n```"
	case protocol.MarkedStringSlice:
		parts := make([]string, 0, len(v))
		for _, marked := range v {
			parts = append(parts, markedStringText(marked))
		}
		return strings.Join(parts, "\n\n")
	default:
		return ""
	}
}

func markedStringText(marked protocol.MarkedString) string {
	switch v := marked.(type) {
	case protocol.String:
		return string(v)
	case *protocol.MarkedStringWithLanguage:
		if v == nil {
			return ""
		}
		if v.Language == "" {
			return v.Value
		}
		return "```" + v.Language + "\n" + v.Value + "\n```"
	default:
		return ""
	}
}

func flattenWorkspaceSymbols(result protocol.WorkspaceSymbolResult) []WorkspaceSymbol {
	switch v := result.(type) {
	case nil:
		return nil
	case protocol.SymbolInformationSlice:
		out := make([]WorkspaceSymbol, 0, len(v))
		for _, sym := range v {
			out = append(out, WorkspaceSymbol{Name: sym.Name, Kind: fmt.Sprint(sym.Kind), ContainerName: stringValue(sym.ContainerName), URI: string(sym.Location.URI), Range: ptrNavigationRange(sym.Location.Range)})
		}
		return out
	case protocol.WorkspaceSymbolSlice:
		out := make([]WorkspaceSymbol, 0, len(v))
		for _, sym := range v {
			item := WorkspaceSymbol{Name: sym.Name, Kind: fmt.Sprint(sym.Kind), ContainerName: stringValue(sym.ContainerName)}
			switch loc := sym.Location.(type) {
			case *protocol.Location:
				if loc != nil {
					item.URI = string(loc.URI)
					item.Range = ptrNavigationRange(loc.Range)
				}
			case *protocol.LocationUriOnly:
				if loc != nil {
					item.URI = string(loc.URI)
				}
			}
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}

func workspaceEditForTextEdits(u uri.URI, edits []protocol.TextEdit) WorkspaceEdit {
	converted := make([]WorkspaceTextEdit, 0, len(edits))
	for _, edit := range edits {
		converted = append(converted, WorkspaceTextEdit{Range: navigationRangeFromProtocol(edit.Range), NewText: edit.NewText})
	}
	if len(converted) == 0 {
		return WorkspaceEdit{}
	}
	return WorkspaceEdit{Changes: map[string][]WorkspaceTextEdit{string(u): converted}}
}

func flattenCodeActionStruct(action *protocol.CodeAction) (CodeAction, error) {
	out := CodeAction{Title: action.Title, IsPreferred: action.IsPreferred}
	if action.Kind != nil {
		out.Kind = string(*action.Kind)
	}
	if action.Disabled.Reason != "" {
		out.DisabledReason = action.Disabled.Reason
	}
	if action.Edit != nil {
		edit, err := WorkspaceEditFromProtocol(*action.Edit)
		if err != nil {
			return CodeAction{}, fmt.Errorf("code action edit: %w", err)
		}
		out.Edit = &edit
	}
	if action.Command.Command != "" || action.Command.Title != "" {
		out.Command = flattenCommand(action.Command)
	}
	return out, nil
}

func flattenCommand(command protocol.Command) *Command {
	tooltip := ""
	if command.Tooltip != nil {
		tooltip = *command.Tooltip
	}
	return &Command{Title: command.Title, Tooltip: tooltip, Command: command.Command}
}

func ptrNavigationRange(rng protocol.Range) *NavigationRange {
	out := navigationRangeFromProtocol(rng)
	return &out
}

func stringValue(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}
