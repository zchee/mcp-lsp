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
	"reflect"
	"strings"
	"time"

	"go.lsp.dev/protocol"
)

// Hover issues textDocument/hover requests through a [Manager].
type Hover struct {
	mgr     *Manager
	timeout time.Duration
}

// Hover returns a textDocument/hover helper for this manager.
func (m *Manager) Hover() *Hover { return &Hover{mgr: m, timeout: defaultTimeout} }

// HoverResult is a compact hover response with zero-based range coordinates.
type HoverResult struct {
	Kind  string
	Value string
	Range *NavigationRange
}

// Lookup opens absPath, synchronizes its text, and returns hover content for pos.
func (h *Hover) Lookup(ctx context.Context, lang, absPath, text string, pos protocol.Position) (*HoverResult, error) {
	ctx, cancel := withRequestTimeout(ctx, h.timeout)
	defer cancel()

	sess, languageID, u, err := h.mgr.sessionForFile(ctx, lang, absPath)
	if err != nil {
		return nil, err
	}
	if !sess.capabilities.hover {
		return nil, fmt.Errorf("hover request: %w", ErrUnsupported)
	}
	if err := sess.syncTextDocument(ctx, u, languageID, text); err != nil {
		return nil, err
	}

	result, err := sess.server.Hover(ctx, &protocol.HoverParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: u},
		Position:     pos,
	})
	if err != nil {
		return nil, fmt.Errorf("hover request: %w", err)
	}
	return flattenHover(result), nil
}

func flattenHover(in *protocol.Hover) *HoverResult {
	if in == nil {
		return nil
	}
	out := &HoverResult{Kind: "plaintext", Value: hoverContentsText(in.Contents)}
	if markup, ok := in.Contents.(*protocol.MarkupContent); ok && markup != nil {
		out.Kind = string(markup.Kind)
		out.Value = markup.Value
	}
	if in.Range != nil {
		rng := navigationRangeFromProtocol(*in.Range)
		out.Range = &rng
	}
	return out
}

func hoverContentsText(contents protocol.HoverContents) string {
	switch v := contents.(type) {
	case nil:
		return ""
	case protocol.String:
		return string(v)
	case *protocol.MarkupContent:
		if v == nil {
			return ""
		}
		return v.Value
	default:
		return legacyMarkedHoverText(v)
	}
}

func legacyMarkedHoverText(value any) string {
	v := reflect.ValueOf(value)
	if !v.IsValid() {
		return ""
	}
	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Pointer:
		if v.IsNil() {
			return ""
		}
		return legacyMarkedHoverStructText(v.Elem())
	case reflect.Slice:
		parts := make([]string, 0, v.Len())
		for i := range v.Len() {
			parts = append(parts, legacyMarkedHoverText(v.Index(i).Interface()))
		}
		return strings.Join(parts, "\n\n")
	default:
		return ""
	}
}

func legacyMarkedHoverStructText(v reflect.Value) string {
	if v.Kind() != reflect.Struct {
		return ""
	}
	value := v.FieldByName("Value")
	if !value.IsValid() || value.Kind() != reflect.String {
		return ""
	}
	text := value.String()
	language := v.FieldByName("Language")
	if !language.IsValid() || language.Kind() != reflect.String || language.String() == "" {
		return text
	}
	return "```" + language.String() + "\n" + text + "\n```"
}
