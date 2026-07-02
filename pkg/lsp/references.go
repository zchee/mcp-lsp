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

// References resolves find-all-references requests through a [Manager]. Its
// timeout bounds the whole request when the caller's context has no deadline.
type References struct {
	mgr     *Manager
	timeout time.Duration
}

// References returns a find-all-references helper for this manager.
func (m *Manager) References() *References {
	return &References{
		mgr:     m,
		timeout: defaultTimeout,
	}
}

// Lookup opens absPath in the language server for lang with the caller-supplied
// text and returns every reference to the symbol at pos, including its
// declaration when includeDeclaration is set. The input position and result
// positions are zero-based. It wraps [ErrUnsupported] when the server does not
// advertise references support.
func (r *References) Lookup(ctx context.Context, lang, absPath, text string, pos protocol.Position, includeDeclaration bool) ([]NavigationLocation, error) {
	ctx, cancel := withRequestTimeout(ctx, r.timeout)
	defer cancel()

	sess, languageID, u, err := r.mgr.sessionForFile(ctx, lang, absPath)
	if err != nil {
		return nil, err
	}
	if !sess.capabilities.references {
		return nil, fmt.Errorf("references request: %w", ErrUnsupported)
	}
	if err := sess.syncTextDocument(ctx, u, languageID, text); err != nil {
		return nil, err
	}

	result, err := sess.server.References(ctx, referenceParams(u, pos, includeDeclaration))
	if err != nil {
		return nil, fmt.Errorf("references request: %w", err)
	}
	out := make([]NavigationLocation, 0, len(result))
	for _, loc := range result {
		out = append(out, navigationLocationFromLocation(loc))
	}
	return out, nil
}

func referenceParams(u uri.URI, pos protocol.Position, includeDeclaration bool) *protocol.ReferenceParams {
	return &protocol.ReferenceParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: u},
			Position:     pos,
		},
		Context: protocol.ReferenceContext{IncludeDeclaration: includeDeclaration},
	}
}
