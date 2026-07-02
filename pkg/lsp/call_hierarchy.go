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

// CallHierarchy resolves call-hierarchy requests through a [Manager]. Its
// timeout bounds each request when the caller's context has no deadline.
//
// Prepare returns the raw [protocol.CallHierarchyItem] values so their Data
// field survives for the follow-up IncomingCalls/OutgoingCalls requests, which
// some servers use to resolve the prepared item; composite layers flatten
// items for agents, this helper does not.
type CallHierarchy struct {
	mgr     *Manager
	timeout time.Duration
}

// CallHierarchy returns a call-hierarchy helper for this manager.
func (m *Manager) CallHierarchy() *CallHierarchy {
	return &CallHierarchy{
		mgr:     m,
		timeout: defaultTimeout,
	}
}

// Prepare opens absPath in the language server for lang with the
// caller-supplied text and returns the call-hierarchy items at pos. The input
// position and item positions are zero-based. It wraps [ErrUnsupported] when
// the server does not advertise call-hierarchy support.
func (c *CallHierarchy) Prepare(ctx context.Context, lang, absPath, text string, pos protocol.Position) ([]protocol.CallHierarchyItem, error) {
	ctx, cancel := withRequestTimeout(ctx, c.timeout)
	defer cancel()

	sess, languageID, u, err := c.mgr.sessionForFile(ctx, lang, absPath)
	if err != nil {
		return nil, err
	}
	if !sess.capabilities.callHierarchy {
		return nil, fmt.Errorf("call hierarchy prepare request: %w", ErrUnsupported)
	}
	if err := sess.syncTextDocument(ctx, u, languageID, text); err != nil {
		return nil, err
	}

	items, err := sess.server.PrepareCallHierarchy(ctx, &protocol.CallHierarchyPrepareParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: u},
			Position:     pos,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("call hierarchy prepare request: %w", err)
	}
	return items, nil
}

// IncomingCalls returns the callers of a prepared item. Pass items exactly as
// Prepare returned them so server-side Data survives the round trip.
func (c *CallHierarchy) IncomingCalls(ctx context.Context, lang string, item *protocol.CallHierarchyItem) ([]protocol.CallHierarchyIncomingCall, error) {
	if item == nil {
		return nil, fmt.Errorf("call hierarchy incoming calls request: item is required")
	}
	ctx, cancel := withRequestTimeout(ctx, c.timeout)
	defer cancel()

	sess, err := c.mgr.session(ctx, lang)
	if err != nil {
		return nil, err
	}
	if !sess.capabilities.callHierarchy {
		return nil, fmt.Errorf("call hierarchy incoming calls request: %w", ErrUnsupported)
	}

	calls, err := sess.server.IncomingCalls(ctx, &protocol.CallHierarchyIncomingCallsParams{Item: *item})
	if err != nil {
		return nil, fmt.Errorf("call hierarchy incoming calls request: %w", err)
	}
	return calls, nil
}

// OutgoingCalls returns the callees of a prepared item. Pass items exactly as
// Prepare returned them so server-side Data survives the round trip.
func (c *CallHierarchy) OutgoingCalls(ctx context.Context, lang string, item *protocol.CallHierarchyItem) ([]protocol.CallHierarchyOutgoingCall, error) {
	if item == nil {
		return nil, fmt.Errorf("call hierarchy outgoing calls request: item is required")
	}
	ctx, cancel := withRequestTimeout(ctx, c.timeout)
	defer cancel()

	sess, err := c.mgr.session(ctx, lang)
	if err != nil {
		return nil, err
	}
	if !sess.capabilities.callHierarchy {
		return nil, fmt.Errorf("call hierarchy outgoing calls request: %w", ErrUnsupported)
	}

	calls, err := sess.server.OutgoingCalls(ctx, &protocol.CallHierarchyOutgoingCallsParams{Item: *item})
	if err != nil {
		return nil, fmt.Errorf("call hierarchy outgoing calls request: %w", err)
	}
	return calls, nil
}
