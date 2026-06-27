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

// Implementation is the goto-implementation feature bound to a [Manager]. Its
// timeout bounds the whole request when the caller's context has no deadline.
type Implementation struct {
	mgr     *Manager
	timeout time.Duration
}

// Implementation returns the goto-implementation feature for this manager.
func (m *Manager) Implementation() *Implementation {
	return &Implementation{
		mgr:     m,
		timeout: defaultTimeout,
	}
}

// Lookup opens absPath in the language server for lang with the caller-supplied
// text and returns the implementation targets for pos. The input position and
// result positions are zero-based.
func (i *Implementation) Lookup(ctx context.Context, lang, absPath, text string, pos protocol.Position) ([]NavigationLocation, error) {
	ctx, cancel := withRequestTimeout(ctx, i.timeout)
	defer cancel()

	sess, err := i.mgr.session(ctx, lang)
	if err != nil {
		return nil, err
	}
	if !sess.implementationSupported {
		return nil, fmt.Errorf("implementation request is not supported by language server")
	}

	cfg := i.mgr.cfg[lang]
	u := uri.File(absPath)
	if err := sess.syncTextDocument(ctx, u, cfg.LanguageID, text); err != nil {
		return nil, err
	}

	result, err := sess.server.Implementation(ctx, implementationParams(u, pos))
	if err != nil {
		return nil, fmt.Errorf("implementation request: %w", err)
	}
	return flattenImplementationResult(result)
}

func implementationParams(u uri.URI, pos protocol.Position) *protocol.ImplementationParams {
	return &protocol.ImplementationParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: u},
		Position:     pos,
	}
}

func flattenImplementationResult(result protocol.DefinitionResult) ([]NavigationLocation, error) {
	return flattenNavigationResult("implementation", result)
}
