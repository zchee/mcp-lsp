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

// FoldingRanges resolves textDocument/foldingRange requests through a
// [Manager]. Its timeout bounds the whole request when the caller's context
// has no deadline.
type FoldingRanges struct {
	mgr     *Manager
	timeout time.Duration
}

// FoldingRanges returns a folding-range helper for this manager.
func (m *Manager) FoldingRanges() *FoldingRanges {
	return &FoldingRanges{
		mgr:     m,
		timeout: defaultTimeout,
	}
}

// FoldingRangeItem is one foldable region with zero-based coordinates. StartLine
// and EndLine are always present; StartColumn and EndColumn are pointers because
// the protocol leaves them undefined for line-level folds, and a nil pointer
// preserves that distinction rather than collapsing it to column zero. Kind is
// the protocol fold kind (e.g. "comment", "region"), empty when the server
// omits it, and CollapsedText is the server-suggested placeholder, empty when
// absent.
type FoldingRangeItem struct {
	StartLine     int
	StartColumn   *int
	EndLine       int
	EndColumn     *int
	Kind          string
	CollapsedText string
}

// Lookup opens absPath in the language server for lang with the caller-supplied
// text and returns the document's foldable regions. Result positions are
// zero-based. It wraps [ErrUnsupported] when the server does not advertise
// folding-range support.
func (f *FoldingRanges) Lookup(ctx context.Context, lang, absPath, text string) ([]FoldingRangeItem, error) {
	ctx, cancel := withRequestTimeout(ctx, f.timeout)
	defer cancel()

	sess, languageID, u, err := f.mgr.sessionForFile(ctx, lang, absPath)
	if err != nil {
		return nil, err
	}
	if !sess.capabilities.foldingRange {
		return nil, fmt.Errorf("folding range request: %w", ErrUnsupported)
	}
	if err := sess.syncTextDocument(ctx, u, languageID, text); err != nil {
		return nil, err
	}

	raw, err := sess.server.FoldingRanges(ctx, foldingRangeParams(u))
	if err != nil {
		return nil, fmt.Errorf("folding range request: %w", err)
	}
	return flattenFoldingRanges(raw), nil
}

func foldingRangeParams(u uri.URI) *protocol.FoldingRangeParams {
	return &protocol.FoldingRangeParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: u},
	}
}

func flattenFoldingRanges(raw []protocol.FoldingRange) []FoldingRangeItem {
	out := make([]FoldingRangeItem, 0, len(raw))
	for i := range raw {
		fr := &raw[i]
		item := FoldingRangeItem{
			StartLine: int(fr.StartLine),
			EndLine:   int(fr.EndLine),
			Kind:      string(fr.Kind),
		}
		if fr.CollapsedText != nil {
			item.CollapsedText = *fr.CollapsedText
		}
		if fr.StartCharacter != nil {
			col := int(*fr.StartCharacter)
			item.StartColumn = &col
		}
		if fr.EndCharacter != nil {
			col := int(*fr.EndCharacter)
			item.EndColumn = &col
		}
		out = append(out, item)
	}
	return out
}
