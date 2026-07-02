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

// DocumentSymbols resolves textDocument/documentSymbol requests through a
// [Manager]. Its timeout bounds the whole request when the caller's context
// has no deadline.
type DocumentSymbols struct {
	mgr     *Manager
	timeout time.Duration
}

// DocumentSymbols returns an in-file symbol outline helper for this manager.
func (m *Manager) DocumentSymbols() *DocumentSymbols {
	return &DocumentSymbols{
		mgr:     m,
		timeout: defaultTimeout,
	}
}

// DocumentSymbolEntry is one node of a document's symbol outline with
// zero-based ranges. Servers answering with the flat SymbolInformation shape
// produce entries whose SelectionRange equals Range and whose Children are
// empty; hierarchical servers populate both distinctly.
type DocumentSymbolEntry struct {
	Name           string
	Detail         string
	Kind           protocol.SymbolKind
	Range          NavigationRange
	SelectionRange NavigationRange
	Children       []DocumentSymbolEntry
}

// Lookup opens absPath in the language server for lang with the caller-supplied
// text and returns the document's symbol outline. Result positions are
// zero-based. It wraps [ErrUnsupported] when the server does not advertise
// document-symbol support.
func (d *DocumentSymbols) Lookup(ctx context.Context, lang, absPath, text string) ([]DocumentSymbolEntry, error) {
	ctx, cancel := withRequestTimeout(ctx, d.timeout)
	defer cancel()

	sess, languageID, u, err := d.mgr.sessionForFile(ctx, lang, absPath)
	if err != nil {
		return nil, err
	}
	if !sess.capabilities.documentSymbol {
		return nil, fmt.Errorf("document symbol request: %w", ErrUnsupported)
	}
	if err := sess.syncTextDocument(ctx, u, languageID, text); err != nil {
		return nil, err
	}

	result, err := sess.server.DocumentSymbol(ctx, documentSymbolParams(u))
	if err != nil {
		return nil, fmt.Errorf("document symbol request: %w", err)
	}
	return flattenDocumentSymbolResult(result)
}

func documentSymbolParams(u uri.URI) *protocol.DocumentSymbolParams {
	return &protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: u},
	}
}

// flattenDocumentSymbolResult converts both shapes of the
// [protocol.DocumentSymbolResult] union: the hierarchical
// [protocol.DocumentSymbolSlice] keeps its child tree, while the flat
// [protocol.SymbolInformationSlice] becomes child-less entries whose
// SelectionRange mirrors Range.
func flattenDocumentSymbolResult(result protocol.DocumentSymbolResult) ([]DocumentSymbolEntry, error) {
	switch r := result.(type) {
	case nil:
		return []DocumentSymbolEntry{}, nil
	case protocol.DocumentSymbolSlice:
		return documentSymbolEntries(r), nil
	case protocol.SymbolInformationSlice:
		out := make([]DocumentSymbolEntry, 0, len(r))
		for _, info := range r {
			rng := navigationRangeFromProtocol(info.Location.Range)
			out = append(out, DocumentSymbolEntry{
				Name:           info.Name,
				Kind:           info.Kind,
				Range:          rng,
				SelectionRange: rng,
			})
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported document symbol result %T", result)
	}
}

func documentSymbolEntries(symbols []protocol.DocumentSymbol) []DocumentSymbolEntry {
	out := make([]DocumentSymbolEntry, 0, len(symbols))
	for _, sym := range symbols {
		entry := DocumentSymbolEntry{
			Name:           sym.Name,
			Kind:           sym.Kind,
			Range:          navigationRangeFromProtocol(sym.Range),
			SelectionRange: navigationRangeFromProtocol(sym.SelectionRange),
		}
		if sym.Detail != nil {
			entry.Detail = *sym.Detail
		}
		if len(sym.Children) > 0 {
			entry.Children = documentSymbolEntries(sym.Children)
		}
		out = append(out, entry)
	}
	return out
}
