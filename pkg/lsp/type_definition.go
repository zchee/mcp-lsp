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

// TypeDefinition resolves goto-type-definition requests through a [Manager].
// Its timeout bounds the whole request when the caller's context has no
// deadline.
type TypeDefinition struct {
	mgr     *Manager
	timeout time.Duration
}

// TypeDefinition returns a goto-type-definition helper for this manager.
func (m *Manager) TypeDefinition() *TypeDefinition {
	return &TypeDefinition{
		mgr:     m,
		timeout: defaultTimeout,
	}
}

// Lookup opens absPath in the language server for lang with the caller-supplied
// text and returns the type-definition targets for pos. The input position and
// result positions are zero-based. It wraps [ErrUnsupported] when the server
// does not advertise type-definition support.
func (t *TypeDefinition) Lookup(ctx context.Context, lang, absPath, text string, pos protocol.Position) ([]NavigationLocation, error) {
	ctx, cancel := withRequestTimeout(ctx, t.timeout)
	defer cancel()

	sess, languageID, u, err := t.mgr.sessionForFile(ctx, lang, absPath)
	if err != nil {
		return nil, err
	}
	if !sess.capabilities.typeDefinition {
		return nil, fmt.Errorf("type definition request: %w", ErrUnsupported)
	}
	if err := sess.syncTextDocument(ctx, u, languageID, text); err != nil {
		return nil, err
	}

	result, err := sess.server.TypeDefinition(ctx, typeDefinitionParams(u, pos))
	if err != nil {
		return nil, fmt.Errorf("type definition request: %w", err)
	}
	return flattenNavigationResult("type definition", result)
}

func typeDefinitionParams(u uri.URI, pos protocol.Position) *protocol.TypeDefinitionParams {
	return &protocol.TypeDefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: u},
			Position:     pos,
		},
	}
}
