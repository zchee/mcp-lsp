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

func (f *fakeServer) PrepareTypeHierarchy(_ context.Context, params *protocol.TypeHierarchyPrepareParams) ([]protocol.TypeHierarchyItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.typeHierarchyPrepareRequests = append(f.typeHierarchyPrepareRequests, *params)
	if f.typeHierarchyErr != nil {
		return nil, f.typeHierarchyErr
	}
	return f.typeHierarchyItems, nil
}

func (f *fakeServer) Supertypes(_ context.Context, params *protocol.TypeHierarchySupertypesParams) ([]protocol.TypeHierarchyItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.supertypesRequests = append(f.supertypesRequests, *params)
	if f.supertypesErr != nil {
		return nil, f.supertypesErr
	}
	return f.supertypes, nil
}

func (f *fakeServer) Subtypes(_ context.Context, params *protocol.TypeHierarchySubtypesParams) ([]protocol.TypeHierarchyItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.subtypesRequests = append(f.subtypesRequests, *params)
	if f.subtypesErr != nil {
		return nil, f.subtypesErr
	}
	return f.subtypes, nil
}

func (f *fakeServer) typeHierarchyPrepareCalls() []protocol.TypeHierarchyPrepareParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.TypeHierarchyPrepareParams(nil), f.typeHierarchyPrepareRequests...)
}

func (f *fakeServer) supertypesCalls() []protocol.TypeHierarchySupertypesParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.TypeHierarchySupertypesParams(nil), f.supertypesRequests...)
}

func (f *fakeServer) subtypesCalls() []protocol.TypeHierarchySubtypesParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.TypeHierarchySubtypesParams(nil), f.subtypesRequests...)
}

func fakeTypeHierarchy(sess *serverSession) *TypeHierarchy {
	mgr := &Manager{
		cfg:      map[string]ServerConfig{"go": {LanguageID: protocol.LanguageKindGo}},
		sessions: map[string]*serverSession{"go": sess},
		logger:   slog.New(slog.DiscardHandler),
	}
	return &TypeHierarchy{mgr: mgr, timeout: 2 * time.Second}
}

func typeHierarchyTestItem() protocol.TypeHierarchyItem {
	return protocol.TypeHierarchyItem{
		Name: "Manager",
		Kind: protocol.SymbolKindStruct,
		URI:  uri.File("/workspace/manager.go"),
		Range: protocol.Range{
			Start: protocol.Position{Line: 10, Character: 0},
			End:   protocol.Position{Line: 30, Character: 1},
		},
		SelectionRange: protocol.Range{
			Start: protocol.Position{Line: 10, Character: 5},
			End:   protocol.Position{Line: 10, Character: 12},
		},
		Data: protocol.LSPAny(`"opaque-server-token"`),
	}
}

func TestTypeHierarchyUnsupportedProvider(t *testing.T) {
	t.Parallel()

	fake := &fakeServer{}
	sess := wireSession(t, fake)
	if sess.capabilities.typeHierarchy {
		t.Fatal("session detected type-hierarchy support that the fake did not advertise")
	}
	th := fakeTypeHierarchy(sess)

	item := typeHierarchyTestItem()
	if _, err := th.Prepare(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Prepare error = %v, want errors.Is ErrUnsupported", err)
	}
	if _, err := th.Supertypes(t.Context(), "go", &item); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Supertypes error = %v, want errors.Is ErrUnsupported", err)
	}
	if _, err := th.Subtypes(t.Context(), "go", &item); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Subtypes error = %v, want errors.Is ErrUnsupported", err)
	}
	if got := len(fake.openedDocs()); got != 0 {
		t.Errorf("unsupported type-hierarchy requests opened %d documents, want 0", got)
	}
	if got := len(fake.typeHierarchyPrepareCalls()) + len(fake.supertypesCalls()) + len(fake.subtypesCalls()); got != 0 {
		t.Errorf("unsupported type-hierarchy issued %d wire requests, want 0", got)
	}
}

func TestTypeHierarchyPrepareReturnsRawItems(t *testing.T) {
	t.Parallel()

	item := typeHierarchyTestItem()
	fake := &fakeServer{typeHierarchySupported: true, typeHierarchyItems: []protocol.TypeHierarchyItem{item}}
	sess := wireSession(t, fake)
	if !sess.capabilities.typeHierarchy {
		t.Fatal("session did not detect type-hierarchy support advertised by the fake")
	}

	pos := protocol.Position{Line: 10, Character: 7}
	got, err := fakeTypeHierarchy(sess).Prepare(t.Context(), "go", "/workspace/main.go", "package main\n", pos)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if diff := gocmp.Diff([]protocol.TypeHierarchyItem{item}, got); diff != "" {
		t.Errorf("Prepare items mismatch (-want +got):\n%s", diff)
	}

	calls := fake.typeHierarchyPrepareCalls()
	if len(calls) != 1 {
		t.Fatalf("expected exactly one prepare request, got %d", len(calls))
	}
	if calls[0].Position != pos {
		t.Errorf("prepare position = %+v, want %+v", calls[0].Position, pos)
	}
}

func TestTypeHierarchySupertypesRoundTripsPreparedItem(t *testing.T) {
	t.Parallel()

	item := typeHierarchyTestItem()
	super := protocol.TypeHierarchyItem{
		Name: "Closer",
		Kind: protocol.SymbolKindInterface,
		URI:  uri.File("/workspace/io.go"),
		Range: protocol.Range{
			Start: protocol.Position{Line: 3, Character: 0},
			End:   protocol.Position{Line: 5, Character: 1},
		},
		SelectionRange: protocol.Range{
			Start: protocol.Position{Line: 3, Character: 5},
			End:   protocol.Position{Line: 3, Character: 11},
		},
	}
	fake := &fakeServer{typeHierarchySupported: true, supertypes: []protocol.TypeHierarchyItem{super}}
	sess := wireSession(t, fake)

	got, err := fakeTypeHierarchy(sess).Supertypes(t.Context(), "go", &item)
	if err != nil {
		t.Fatalf("Supertypes: %v", err)
	}
	if diff := gocmp.Diff([]protocol.TypeHierarchyItem{super}, got); diff != "" {
		t.Errorf("Supertypes mismatch (-want +got):\n%s", diff)
	}

	calls := fake.supertypesCalls()
	if len(calls) != 1 {
		t.Fatalf("expected exactly one supertypes request, got %d", len(calls))
	}
	if diff := gocmp.Diff(item, calls[0].Item); diff != "" {
		t.Errorf("supertypes item did not round-trip Data (-want +got):\n%s", diff)
	}
}

func TestTypeHierarchySubtypesRoundTripsPreparedItem(t *testing.T) {
	t.Parallel()

	item := typeHierarchyTestItem()
	sub := protocol.TypeHierarchyItem{
		Name: "recordingManager",
		Kind: protocol.SymbolKindStruct,
		URI:  uri.File("/workspace/manager_test.go"),
		Range: protocol.Range{
			Start: protocol.Position{Line: 50, Character: 0},
			End:   protocol.Position{Line: 60, Character: 1},
		},
		SelectionRange: protocol.Range{
			Start: protocol.Position{Line: 50, Character: 5},
			End:   protocol.Position{Line: 50, Character: 21},
		},
	}
	fake := &fakeServer{typeHierarchySupported: true, subtypes: []protocol.TypeHierarchyItem{sub}}
	sess := wireSession(t, fake)

	got, err := fakeTypeHierarchy(sess).Subtypes(t.Context(), "go", &item)
	if err != nil {
		t.Fatalf("Subtypes: %v", err)
	}
	if diff := gocmp.Diff([]protocol.TypeHierarchyItem{sub}, got); diff != "" {
		t.Errorf("Subtypes mismatch (-want +got):\n%s", diff)
	}

	calls := fake.subtypesCalls()
	if len(calls) != 1 {
		t.Fatalf("expected exactly one subtypes request, got %d", len(calls))
	}
	if diff := gocmp.Diff(item, calls[0].Item); diff != "" {
		t.Errorf("subtypes item did not round-trip Data (-want +got):\n%s", diff)
	}
}

func TestTypeHierarchyPrepareEmptyResult(t *testing.T) {
	t.Parallel()

	sess := wireSession(t, &fakeServer{typeHierarchySupported: true})
	got, err := fakeTypeHierarchy(sess).Prepare(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Prepare returned %d items, want 0: %+v", len(got), got)
	}
}

func TestTypeHierarchySurfacesServerErrors(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("server unavailable")
	fake := &fakeServer{
		typeHierarchySupported: true,
		typeHierarchyErr:       sentinel,
		supertypesErr:          sentinel,
		subtypesErr:            sentinel,
	}
	sess := wireSession(t, fake)
	th := fakeTypeHierarchy(sess)

	item := typeHierarchyTestItem()
	if _, err := th.Prepare(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{}); err == nil || !strings.Contains(err.Error(), "type hierarchy prepare request") {
		t.Fatalf("Prepare error = %v, want type hierarchy prepare request context", err)
	}
	if _, err := th.Supertypes(t.Context(), "go", &item); err == nil || !strings.Contains(err.Error(), "type hierarchy supertypes request") {
		t.Fatalf("Supertypes error = %v, want type hierarchy supertypes request context", err)
	}
	if _, err := th.Subtypes(t.Context(), "go", &item); err == nil || !strings.Contains(err.Error(), "type hierarchy subtypes request") {
		t.Fatalf("Subtypes error = %v, want type hierarchy subtypes request context", err)
	}
}
