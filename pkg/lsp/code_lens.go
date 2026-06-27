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
	ctx, cancel := withRequestTimeout(ctx, c.timeout)
	defer cancel()

	sess, languageID, u, err := c.mgr.sessionForFile(ctx, lang, absPath)
	if err != nil {
		return nil, err
	}
	if !sess.capabilities.codeLens {
		return nil, fmt.Errorf("code lens request is not supported by language server")
	}
	if err := sess.syncTextDocument(ctx, u, languageID, text); err != nil {
		return nil, err
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
