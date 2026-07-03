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
	"strings"
	"time"

	"go.lsp.dev/protocol"
)

// InlayHints resolves inlay hint requests through a [Manager]. Its timeout
// bounds the whole request when the caller's context has no deadline.
type InlayHints struct {
	mgr     *Manager
	timeout time.Duration
}

// InlayHints returns an inlay hint helper for this manager.
func (m *Manager) InlayHints() *InlayHints {
	return &InlayHints{mgr: m, timeout: defaultTimeout}
}

// InlayHintItem is a flattened inlay hint at a zero-based position. Kind is
// "type", "parameter", or "" when the server did not classify the hint.
type InlayHintItem struct {
	Line   int
	Column int
	Label  string
	Kind   string
}

// Lookup opens absPath in the language server for lang with the caller-supplied
// text and returns every inlay hint within rng. The input range and result
// positions are zero-based. It wraps [ErrUnsupported] when the server does not
// advertise inlay hint support.
func (i *InlayHints) Lookup(ctx context.Context, lang, absPath, text string, rng protocol.Range) ([]InlayHintItem, error) {
	ctx, cancel := withRequestTimeout(ctx, i.timeout)
	defer cancel()

	sess, languageID, u, err := i.mgr.sessionForFile(ctx, lang, absPath)
	if err != nil {
		return nil, err
	}
	if !sess.capabilities.inlayHint {
		return nil, fmt.Errorf("inlay hint request: %w", ErrUnsupported)
	}
	if err := sess.syncTextDocument(ctx, u, languageID, text); err != nil {
		return nil, err
	}

	result, err := sess.server.InlayHint(ctx, &protocol.InlayHintParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: u},
		Range:        rng,
	})
	if err != nil {
		return nil, fmt.Errorf("inlay hint request: %w", err)
	}
	out := make([]InlayHintItem, 0, len(result))
	for i := range result {
		hint := &result[i]
		out = append(out, InlayHintItem{
			Line:   int(hint.Position.Line),
			Column: int(hint.Position.Character),
			Label:  inlayHintLabel(hint.Label),
			Kind:   inlayHintKind(hint.Kind),
		})
	}
	return out, nil
}

func inlayHintKind(k protocol.InlayHintKind) string {
	switch k {
	case protocol.InlayHintKindType:
		return "type"
	case protocol.InlayHintKindParameter:
		return "parameter"
	default:
		return ""
	}
}

func inlayHintLabel(l protocol.InlayHintLabel) string {
	switch v := l.(type) {
	case protocol.String:
		return string(v)
	case protocol.InlayHintLabelPartSlice:
		var b strings.Builder
		for i := range v {
			b.WriteString(v[i].Value)
		}
		return b.String()
	default:
		return ""
	}
}
