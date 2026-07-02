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

// TypeHierarchy resolves type-hierarchy requests through a [Manager]. Its
// timeout bounds each request when the caller's context has no deadline.
//
// Prepare returns the raw [protocol.TypeHierarchyItem] values so their Data
// field survives for the follow-up Supertypes/Subtypes requests, which some
// servers use to resolve the prepared item; composite layers flatten items for
// agents, this helper does not.
type TypeHierarchy struct {
	mgr     *Manager
	timeout time.Duration
}

// TypeHierarchy returns a type-hierarchy helper for this manager.
func (m *Manager) TypeHierarchy() *TypeHierarchy {
	return &TypeHierarchy{
		mgr:     m,
		timeout: defaultTimeout,
	}
}

// Prepare opens absPath in the language server for lang with the
// caller-supplied text and returns the type-hierarchy items at pos. The input
// position and item positions are zero-based. It wraps [ErrUnsupported] when
// the server does not advertise type-hierarchy support.
func (t *TypeHierarchy) Prepare(ctx context.Context, lang, absPath, text string, pos protocol.Position) ([]protocol.TypeHierarchyItem, error) {
	ctx, cancel := withRequestTimeout(ctx, t.timeout)
	defer cancel()

	sess, languageID, u, err := t.mgr.sessionForFile(ctx, lang, absPath)
	if err != nil {
		return nil, err
	}
	if !sess.capabilities.typeHierarchy {
		return nil, fmt.Errorf("type hierarchy prepare request: %w", ErrUnsupported)
	}
	if err := sess.syncTextDocument(ctx, u, languageID, text); err != nil {
		return nil, err
	}

	items, err := sess.server.PrepareTypeHierarchy(ctx, &protocol.TypeHierarchyPrepareParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: u},
			Position:     pos,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("type hierarchy prepare request: %w", err)
	}
	return items, nil
}

// Supertypes returns the supertypes of a prepared item. Pass items exactly as
// Prepare returned them so server-side Data survives the round trip.
func (t *TypeHierarchy) Supertypes(ctx context.Context, lang string, item *protocol.TypeHierarchyItem) ([]protocol.TypeHierarchyItem, error) {
	if item == nil {
		return nil, fmt.Errorf("type hierarchy supertypes request: item is required")
	}
	ctx, cancel := withRequestTimeout(ctx, t.timeout)
	defer cancel()

	sess, err := t.mgr.session(ctx, lang)
	if err != nil {
		return nil, err
	}
	if !sess.capabilities.typeHierarchy {
		return nil, fmt.Errorf("type hierarchy supertypes request: %w", ErrUnsupported)
	}

	items, err := sess.server.Supertypes(ctx, &protocol.TypeHierarchySupertypesParams{Item: *item})
	if err != nil {
		return nil, fmt.Errorf("type hierarchy supertypes request: %w", err)
	}
	return items, nil
}

// Subtypes returns the subtypes of a prepared item. Pass items exactly as
// Prepare returned them so server-side Data survives the round trip.
func (t *TypeHierarchy) Subtypes(ctx context.Context, lang string, item *protocol.TypeHierarchyItem) ([]protocol.TypeHierarchyItem, error) {
	if item == nil {
		return nil, fmt.Errorf("type hierarchy subtypes request: item is required")
	}
	ctx, cancel := withRequestTimeout(ctx, t.timeout)
	defer cancel()

	sess, err := t.mgr.session(ctx, lang)
	if err != nil {
		return nil, err
	}
	if !sess.capabilities.typeHierarchy {
		return nil, fmt.Errorf("type hierarchy subtypes request: %w", ErrUnsupported)
	}

	items, err := sess.server.Subtypes(ctx, &protocol.TypeHierarchySubtypesParams{Item: *item})
	if err != nil {
		return nil, fmt.Errorf("type hierarchy subtypes request: %w", err)
	}
	return items, nil
}
