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

	sess, languageID, u, err := r.mgr.sessionForFile(ctx, lang, absPath)
	if err != nil {
		return WorkspaceEdit{}, err
	}
	if !sess.capabilities.rename {
		return WorkspaceEdit{}, fmt.Errorf("rename request is not supported by language server")
	}
	if err := sess.syncTextDocument(ctx, u, languageID, text); err != nil {
		return WorkspaceEdit{}, err
	}
	edit, err := sess.server.Rename(ctx, &protocol.RenameParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: u},
		Position:     pos,
		NewName:      newName,
	})
	if err != nil {
		return WorkspaceEdit{}, fmt.Errorf("rename request: %w", err)
	}
	if edit == nil {
		return WorkspaceEdit{}, nil
	}
	return WorkspaceEditFromProtocol(*edit)
}
