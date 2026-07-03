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

// DocumentHighlight resolves textDocument/documentHighlight requests through a
// [Manager]. Its timeout bounds the whole request when the caller's context
// has no deadline.
type DocumentHighlight struct {
	mgr     *Manager
	timeout time.Duration
}

// DocumentHighlight returns a document-highlight helper for this manager.
func (m *Manager) DocumentHighlight() *DocumentHighlight {
	return &DocumentHighlight{
		mgr:     m,
		timeout: defaultTimeout,
	}
}

// DocumentHighlightSpan is one highlighted occurrence of the symbol at a
// queried position, with a zero-based Range. Kind is one of "text", "read",
// or "write".
type DocumentHighlightSpan struct {
	Range NavigationRange
	Kind  string
}

// Lookup opens absPath in the language server for lang with the caller-supplied
// text and returns every highlight for the symbol at pos, such as its
// declaration and read/write occurrences. The input position and result
// positions are zero-based. It wraps [ErrUnsupported] when the server does not
// advertise document-highlight support.
func (d *DocumentHighlight) Lookup(ctx context.Context, lang, absPath, text string, pos protocol.Position) ([]DocumentHighlightSpan, error) {
	ctx, cancel := withRequestTimeout(ctx, d.timeout)
	defer cancel()

	sess, languageID, u, err := d.mgr.sessionForFile(ctx, lang, absPath)
	if err != nil {
		return nil, err
	}
	if !sess.capabilities.documentHighlight {
		return nil, fmt.Errorf("document highlight request: %w", ErrUnsupported)
	}
	if err := sess.syncTextDocument(ctx, u, languageID, text); err != nil {
		return nil, err
	}

	result, err := sess.server.DocumentHighlight(ctx, &protocol.DocumentHighlightParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: u},
			Position:     pos,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("document highlight request: %w", err)
	}
	out := make([]DocumentHighlightSpan, 0, len(result))
	for _, hl := range result {
		out = append(out, DocumentHighlightSpan{
			Range: navigationRangeFromProtocol(hl.Range),
			Kind:  documentHighlightKind(hl.Kind),
		})
	}
	return out, nil
}

// documentHighlightKind maps a [protocol.DocumentHighlightKind] to its string
// form, defaulting to "text" per the LSP spec when the kind is absent.
func documentHighlightKind(k protocol.DocumentHighlightKind) string {
	switch k {
	case protocol.DocumentHighlightKindRead:
		return "read"
	case protocol.DocumentHighlightKindWrite:
		return "write"
	default:
		return "text"
	}
}
