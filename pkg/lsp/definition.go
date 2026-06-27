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

// Definition is the goto-definition feature bound to a [Manager]. Its timeout
// bounds the whole request when the caller's context has no deadline.
type Definition struct {
	mgr     *Manager
	timeout time.Duration
}

// Definition returns the goto-definition feature for this manager.
func (m *Manager) Definition() *Definition {
	return &Definition{
		mgr:     m,
		timeout: defaultTimeout,
	}
}

// Lookup opens absPath in the language server for lang with the caller-supplied
// text and returns the definition targets for pos. The input position and result
// positions are zero-based.
func (d *Definition) Lookup(ctx context.Context, lang, absPath, text string, pos protocol.Position) ([]NavigationLocation, error) {
	ctx, cancel := withRequestTimeout(ctx, d.timeout)
	defer cancel()

	sess, err := d.mgr.session(ctx, lang)
	if err != nil {
		return nil, err
	}

	cfg := d.mgr.cfg[lang]
	u := uri.File(absPath)
	if err := sess.syncTextDocument(ctx, u, cfg.LanguageID, text); err != nil {
		return nil, err
	}

	result, err := sess.server.Definition(ctx, definitionParams(u, pos))
	if err != nil {
		return nil, fmt.Errorf("definition request: %w", err)
	}
	return flattenDefinitionResult(result)
}

func definitionParams(u uri.URI, pos protocol.Position) *protocol.DefinitionParams {
	return &protocol.DefinitionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: u},
		Position:     pos,
	}
}

func flattenDefinitionResult(result protocol.DefinitionResult) ([]NavigationLocation, error) {
	return flattenNavigationResult("definition", result)
}
