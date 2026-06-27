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
	"time"

	"go.lsp.dev/protocol"
)

// CodeActions is the textDocument/codeAction feature bound to a [Manager].
type CodeActions struct {
	mgr     *Manager
	timeout time.Duration
}

// CodeActions returns the code action feature for this manager.
func (m *Manager) CodeActions() *CodeActions { return &CodeActions{mgr: m, timeout: defaultTimeout} }

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
	ctx, cancel := withRequestTimeout(ctx, c.timeout)
	defer cancel()

	sess, languageID, u, err := c.mgr.sessionForFile(ctx, lang, absPath)
	if err != nil {
		return nil, err
	}
	if !sess.capabilities.codeAction {
		return nil, fmt.Errorf("code action request is not supported by language server")
	}
	if err := sess.syncTextDocument(ctx, u, languageID, text); err != nil {
		return nil, err
	}
	raw, err := sess.server.CodeAction(
		ctx, &protocol.CodeActionParams{
			TextDocument: protocol.TextDocumentIdentifier{
				URI: u,
			},
			Range: rng,
			Context: protocol.CodeActionContext{
				Diagnostics: []protocol.Diagnostic{},
				Only:        only,
			},
		},
	)
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
