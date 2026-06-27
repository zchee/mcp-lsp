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
	"go.lsp.dev/uri"
)

// Formatting is the textDocument formatting feature bound to a [Manager].
type Formatting struct {
	mgr     *Manager
	timeout time.Duration
}

// Formatting returns the formatting feature for this manager.
func (m *Manager) Formatting() *Formatting { return &Formatting{mgr: m, timeout: defaultTimeout} }

// Format returns a workspace edit preview for textDocument/formatting.
func (f *Formatting) Format(ctx context.Context, lang, absPath, text string, options protocol.FormattingOptions) (WorkspaceEdit, error) {
	ctx, cancel := withRequestTimeout(ctx, f.timeout)
	defer cancel()

	sess, languageID, u, err := f.mgr.sessionForFile(ctx, lang, absPath)
	if err != nil {
		return WorkspaceEdit{}, err
	}
	if !sess.capabilities.formatting {
		return WorkspaceEdit{}, fmt.Errorf("formatting request is not supported by language server")
	}
	if err := sess.syncTextDocument(ctx, u, languageID, text); err != nil {
		return WorkspaceEdit{}, err
	}
	edits, err := sess.server.Formatting(ctx, &protocol.DocumentFormattingParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}, Options: options})
	if err != nil {
		return WorkspaceEdit{}, fmt.Errorf("formatting request: %w", err)
	}
	return workspaceEditForTextEdits(u, edits), nil
}

// RangeFormat returns a workspace edit preview for textDocument/rangeFormatting.
func (f *Formatting) RangeFormat(ctx context.Context, lang, absPath, text string, rng protocol.Range, options protocol.FormattingOptions) (WorkspaceEdit, error) {
	ctx, cancel := withRequestTimeout(ctx, f.timeout)
	defer cancel()

	sess, languageID, u, err := f.mgr.sessionForFile(ctx, lang, absPath)
	if err != nil {
		return WorkspaceEdit{}, err
	}
	if !sess.capabilities.rangeFormatting {
		return WorkspaceEdit{}, fmt.Errorf("range formatting request is not supported by language server")
	}
	if err := sess.syncTextDocument(ctx, u, languageID, text); err != nil {
		return WorkspaceEdit{}, err
	}
	edits, err := sess.server.RangeFormatting(ctx, &protocol.DocumentRangeFormattingParams{TextDocument: protocol.TextDocumentIdentifier{URI: u}, Range: rng, Options: options})
	if err != nil {
		return WorkspaceEdit{}, fmt.Errorf("range formatting request: %w", err)
	}
	return workspaceEditForTextEdits(u, edits), nil
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
