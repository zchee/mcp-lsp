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
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	gocmp "github.com/google/go-cmp/cmp"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func (f *fakeServer) PrepareCallHierarchy(_ context.Context, params *protocol.CallHierarchyPrepareParams) ([]protocol.CallHierarchyItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.callHierarchyPrepareRequests = append(f.callHierarchyPrepareRequests, *params)
	if f.callHierarchyErr != nil {
		return nil, f.callHierarchyErr
	}
	return f.callHierarchyItems, nil
}

func (f *fakeServer) IncomingCalls(_ context.Context, params *protocol.CallHierarchyIncomingCallsParams) ([]protocol.CallHierarchyIncomingCall, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.incomingCallsRequests = append(f.incomingCallsRequests, *params)
	if f.incomingCallsErr != nil {
		return nil, f.incomingCallsErr
	}
	return f.incomingCalls, nil
}

func (f *fakeServer) OutgoingCalls(_ context.Context, params *protocol.CallHierarchyOutgoingCallsParams) ([]protocol.CallHierarchyOutgoingCall, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.outgoingCallsRequests = append(f.outgoingCallsRequests, *params)
	if f.outgoingCallsErr != nil {
		return nil, f.outgoingCallsErr
	}
	return f.outgoingCalls, nil
}

func (f *fakeServer) callHierarchyPrepareCalls() []protocol.CallHierarchyPrepareParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.CallHierarchyPrepareParams(nil), f.callHierarchyPrepareRequests...)
}

func (f *fakeServer) incomingCallsCalls() []protocol.CallHierarchyIncomingCallsParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.CallHierarchyIncomingCallsParams(nil), f.incomingCallsRequests...)
}

func (f *fakeServer) outgoingCallsCalls() []protocol.CallHierarchyOutgoingCallsParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.CallHierarchyOutgoingCallsParams(nil), f.outgoingCallsRequests...)
}

func fakeCallHierarchy(sess *serverSession) *CallHierarchy {
	mgr := &Manager{
		cfg:      map[string]ServerConfig{"go": {LanguageID: protocol.LanguageKindGo}},
		sessions: map[string]*serverSession{"go": sess},
		logger:   slog.New(slog.DiscardHandler),
	}
	return &CallHierarchy{mgr: mgr, timeout: 2 * time.Second}
}

func callHierarchyTestItem() protocol.CallHierarchyItem {
	return protocol.CallHierarchyItem{
		Name: "Close",
		Kind: protocol.SymbolKindMethod,
		URI:  uri.File("/workspace/manager.go"),
		Range: protocol.Range{
			Start: protocol.Position{Line: 20, Character: 0},
			End:   protocol.Position{Line: 24, Character: 1},
		},
		SelectionRange: protocol.Range{
			Start: protocol.Position{Line: 20, Character: 18},
			End:   protocol.Position{Line: 20, Character: 23},
		},
		Data: protocol.LSPAny(`"opaque-server-token"`),
	}
}

func TestCallHierarchyUnsupportedProvider(t *testing.T) {
	t.Parallel()

	fake := &fakeServer{}
	sess := wireSession(t, fake)
	if sess.capabilities.callHierarchy {
		t.Fatal("session detected call-hierarchy support that the fake did not advertise")
	}
	ch := fakeCallHierarchy(sess)

	item := callHierarchyTestItem()
	if _, err := ch.Prepare(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Prepare error = %v, want errors.Is ErrUnsupported", err)
	}
	if _, err := ch.IncomingCalls(t.Context(), "go", &item); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("IncomingCalls error = %v, want errors.Is ErrUnsupported", err)
	}
	if _, err := ch.OutgoingCalls(t.Context(), "go", &item); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("OutgoingCalls error = %v, want errors.Is ErrUnsupported", err)
	}
	if got := len(fake.openedDocs()); got != 0 {
		t.Errorf("unsupported call-hierarchy requests opened %d documents, want 0", got)
	}
	if got := len(fake.callHierarchyPrepareCalls()) + len(fake.incomingCallsCalls()) + len(fake.outgoingCallsCalls()); got != 0 {
		t.Errorf("unsupported call-hierarchy issued %d wire requests, want 0", got)
	}
}

func TestCallHierarchyPrepareReturnsRawItems(t *testing.T) {
	t.Parallel()

	item := callHierarchyTestItem()
	fake := &fakeServer{callHierarchySupported: true, callHierarchyItems: []protocol.CallHierarchyItem{item}}
	sess := wireSession(t, fake)
	if !sess.capabilities.callHierarchy {
		t.Fatal("session did not detect call-hierarchy support advertised by the fake")
	}

	pos := protocol.Position{Line: 20, Character: 19}
	got, err := fakeCallHierarchy(sess).Prepare(t.Context(), "go", "/workspace/main.go", "package main\n", pos)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if diff := gocmp.Diff([]protocol.CallHierarchyItem{item}, got); diff != "" {
		t.Errorf("Prepare items mismatch (-want +got):\n%s", diff)
	}

	calls := fake.callHierarchyPrepareCalls()
	if len(calls) != 1 {
		t.Fatalf("expected exactly one prepare request, got %d", len(calls))
	}
	if calls[0].Position != pos {
		t.Errorf("prepare position = %+v, want %+v", calls[0].Position, pos)
	}
}

func TestCallHierarchyIncomingCallsRoundTripsPreparedItem(t *testing.T) {
	t.Parallel()

	item := callHierarchyTestItem()
	caller := protocol.CallHierarchyItem{
		Name: "Run",
		Kind: protocol.SymbolKindFunction,
		URI:  uri.File("/workspace/cmd.go"),
		Range: protocol.Range{
			Start: protocol.Position{Line: 5, Character: 0},
			End:   protocol.Position{Line: 9, Character: 1},
		},
		SelectionRange: protocol.Range{
			Start: protocol.Position{Line: 5, Character: 5},
			End:   protocol.Position{Line: 5, Character: 8},
		},
	}
	fake := &fakeServer{
		callHierarchySupported: true,
		incomingCalls: []protocol.CallHierarchyIncomingCall{
			{
				From: caller,
				FromRanges: []protocol.Range{{
					Start: protocol.Position{Line: 7, Character: 2},
					End:   protocol.Position{Line: 7, Character: 7},
				}},
			},
		},
	}
	sess := wireSession(t, fake)

	got, err := fakeCallHierarchy(sess).IncomingCalls(t.Context(), "go", &item)
	if err != nil {
		t.Fatalf("IncomingCalls: %v", err)
	}
	if diff := gocmp.Diff(fake.incomingCalls, got); diff != "" {
		t.Errorf("IncomingCalls mismatch (-want +got):\n%s", diff)
	}

	calls := fake.incomingCallsCalls()
	if len(calls) != 1 {
		t.Fatalf("expected exactly one incoming-calls request, got %d", len(calls))
	}
	if diff := gocmp.Diff(item, calls[0].Item); diff != "" {
		t.Errorf("incoming-calls item did not round-trip Data (-want +got):\n%s", diff)
	}
}

func TestCallHierarchyOutgoingCallsRoundTripsPreparedItem(t *testing.T) {
	t.Parallel()

	item := callHierarchyTestItem()
	callee := protocol.CallHierarchyItem{
		Name: "shutdown",
		Kind: protocol.SymbolKindMethod,
		URI:  uri.File("/workspace/session.go"),
		Range: protocol.Range{
			Start: protocol.Position{Line: 40, Character: 0},
			End:   protocol.Position{Line: 44, Character: 1},
		},
		SelectionRange: protocol.Range{
			Start: protocol.Position{Line: 40, Character: 22},
			End:   protocol.Position{Line: 40, Character: 30},
		},
	}
	fake := &fakeServer{
		callHierarchySupported: true,
		outgoingCalls: []protocol.CallHierarchyOutgoingCall{
			{
				To: callee,
				FromRanges: []protocol.Range{{
					Start: protocol.Position{Line: 22, Character: 2},
					End:   protocol.Position{Line: 22, Character: 10},
				}},
			},
		},
	}
	sess := wireSession(t, fake)

	got, err := fakeCallHierarchy(sess).OutgoingCalls(t.Context(), "go", &item)
	if err != nil {
		t.Fatalf("OutgoingCalls: %v", err)
	}
	if diff := gocmp.Diff(fake.outgoingCalls, got); diff != "" {
		t.Errorf("OutgoingCalls mismatch (-want +got):\n%s", diff)
	}

	calls := fake.outgoingCallsCalls()
	if len(calls) != 1 {
		t.Fatalf("expected exactly one outgoing-calls request, got %d", len(calls))
	}
	if diff := gocmp.Diff(item, calls[0].Item); diff != "" {
		t.Errorf("outgoing-calls item did not round-trip Data (-want +got):\n%s", diff)
	}
}

func TestCallHierarchyPrepareEmptyResult(t *testing.T) {
	t.Parallel()

	sess := wireSession(t, &fakeServer{callHierarchySupported: true})
	got, err := fakeCallHierarchy(sess).Prepare(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Prepare returned %d items, want 0: %+v", len(got), got)
	}
}

func TestCallHierarchySurfacesServerErrors(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("server unavailable")
	fake := &fakeServer{
		callHierarchySupported: true,
		callHierarchyErr:       sentinel,
		incomingCallsErr:       sentinel,
		outgoingCallsErr:       sentinel,
	}
	sess := wireSession(t, fake)
	ch := fakeCallHierarchy(sess)

	item := callHierarchyTestItem()
	if _, err := ch.Prepare(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{}); err == nil || !strings.Contains(err.Error(), "call hierarchy prepare request") {
		t.Fatalf("Prepare error = %v, want call hierarchy prepare request context", err)
	}
	if _, err := ch.IncomingCalls(t.Context(), "go", &item); err == nil || !strings.Contains(err.Error(), "call hierarchy incoming calls request") {
		t.Fatalf("IncomingCalls error = %v, want call hierarchy incoming calls request context", err)
	}
	if _, err := ch.OutgoingCalls(t.Context(), "go", &item); err == nil || !strings.Contains(err.Error(), "call hierarchy outgoing calls request") {
		t.Fatalf("OutgoingCalls error = %v, want call hierarchy outgoing calls request context", err)
	}
}
