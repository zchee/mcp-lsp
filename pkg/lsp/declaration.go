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

// Declaration resolves goto-declaration requests through a [Manager]. Its
// timeout bounds the whole request when the caller's context has no deadline.
type Declaration struct {
	mgr     *Manager
	timeout time.Duration
}

// Declaration returns a goto-declaration helper for this manager.
func (m *Manager) Declaration() *Declaration {
	return &Declaration{
		mgr:     m,
		timeout: defaultTimeout,
	}
}

// Lookup opens absPath in the language server for lang with the caller-supplied
// text and returns the declaration targets for pos. The input position and
// result positions are zero-based. It wraps [ErrUnsupported] when the server
// does not advertise declaration support.
func (d *Declaration) Lookup(ctx context.Context, lang, absPath, text string, pos protocol.Position) ([]NavigationLocation, error) {
	ctx, cancel := withRequestTimeout(ctx, d.timeout)
	defer cancel()

	sess, languageID, u, err := d.mgr.sessionForFile(ctx, lang, absPath)
	if err != nil {
		return nil, err
	}
	if !sess.capabilities.declaration {
		return nil, fmt.Errorf("declaration request: %w", ErrUnsupported)
	}
	if err := sess.syncTextDocument(ctx, u, languageID, text); err != nil {
		return nil, err
	}

	result, err := sess.server.Declaration(ctx, declarationParams(u, pos))
	if err != nil {
		return nil, fmt.Errorf("declaration request: %w", err)
	}
	return flattenDeclarationResult(result)
}

func declarationParams(u uri.URI, pos protocol.Position) *protocol.DeclarationParams {
	return &protocol.DeclarationParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: u},
			Position:     pos,
		},
	}
}

// flattenDeclarationResult mirrors flattenNavigationResult for the
// [protocol.DeclarationResult] union, whose link slice carries
// [protocol.DeclarationLink] instead of [protocol.DefinitionLink]; both name
// the same underlying [protocol.LocationLink].
func flattenDeclarationResult(result protocol.DeclarationResult) ([]NavigationLocation, error) {
	switch r := result.(type) {
	case nil:
		return []NavigationLocation{}, nil
	case *protocol.Location:
		if r == nil {
			return []NavigationLocation{}, nil
		}
		return []NavigationLocation{navigationLocationFromLocation(*r)}, nil
	case protocol.LocationSlice:
		out := make([]NavigationLocation, 0, len(r))
		for _, loc := range r {
			out = append(out, navigationLocationFromLocation(loc))
		}
		return out, nil
	case protocol.DeclarationLinkSlice:
		out := make([]NavigationLocation, 0, len(r))
		for _, link := range r {
			out = append(out, navigationLocationFromLink(protocol.DefinitionLink(link)))
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported declaration result %T", result)
	}
}
