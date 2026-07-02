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

func (f *fakeServer) TypeDefinition(_ context.Context, params *protocol.TypeDefinitionParams) (protocol.DefinitionResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.typeDefinitionRequests = append(f.typeDefinitionRequests, *params)
	if f.typeDefinitionErr != nil {
		return nil, f.typeDefinitionErr
	}
	return f.typeDefinitionResult, nil
}

func (f *fakeServer) typeDefinitionCalls() []protocol.TypeDefinitionParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.TypeDefinitionParams(nil), f.typeDefinitionRequests...)
}

func fakeTypeDefinition(sess *serverSession) *TypeDefinition {
	mgr := &Manager{
		cfg:      map[string]ServerConfig{"go": {LanguageID: protocol.LanguageKindGo}},
		sessions: map[string]*serverSession{"go": sess},
		logger:   slog.New(slog.DiscardHandler),
	}
	return &TypeDefinition{mgr: mgr, timeout: 2 * time.Second}
}

func TestTypeDefinitionLookupUnsupportedProvider(t *testing.T) {
	t.Parallel()

	fake := &fakeServer{}
	sess := wireSession(t, fake)
	if sess.capabilities.typeDefinition {
		t.Fatal("session detected type-definition support that the fake did not advertise")
	}

	_, err := fakeTypeDefinition(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Lookup error = %v, want errors.Is ErrUnsupported", err)
	}
	if !strings.Contains(err.Error(), "type definition request") {
		t.Fatalf("Lookup error = %v, want the failing primitive named", err)
	}
	if got := len(fake.openedDocs()); got != 0 {
		t.Errorf("Lookup opened %d documents despite unsupported provider, want 0", got)
	}
	if got := len(fake.typeDefinitionCalls()); got != 0 {
		t.Errorf("Lookup issued %d type-definition requests despite unsupported provider, want 0", got)
	}
}

func TestTypeDefinitionLookupLocation(t *testing.T) {
	t.Parallel()

	targetURI := uri.File("/workspace/types.go")
	fake := &fakeServer{
		typeDefinitionSupported: true,
		typeDefinitionResult: &protocol.Location{
			URI: targetURI,
			Range: protocol.Range{
				Start: protocol.Position{Line: 7, Character: 1},
				End:   protocol.Position{Line: 7, Character: 6},
			},
		},
	}
	sess := wireSession(t, fake)
	if !sess.capabilities.typeDefinition {
		t.Fatal("session did not detect type-definition support advertised by the fake")
	}

	got, err := fakeTypeDefinition(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{Line: 3, Character: 4})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	wantRange := NavigationRange{StartLine: 7, StartColumn: 1, EndLine: 7, EndColumn: 6}
	want := []NavigationLocation{
		{TargetURI: string(targetURI), TargetRange: wantRange, TargetSelectionRange: wantRange},
	}
	if diff := gocmp.Diff(want, got); diff != "" {
		t.Errorf("Lookup type definition mismatch (-want +got):\n%s", diff)
	}
}

func TestTypeDefinitionLookupDefinitionLinkSlice(t *testing.T) {
	t.Parallel()

	targetURI := uri.File("/workspace/linked_types.go")
	fake := &fakeServer{
		typeDefinitionSupported: true,
		typeDefinitionResult: protocol.DefinitionLinkSlice{
			{
				TargetURI: targetURI,
				TargetRange: protocol.Range{
					Start: protocol.Position{Line: 20, Character: 0},
					End:   protocol.Position{Line: 24, Character: 1},
				},
				TargetSelectionRange: protocol.Range{
					Start: protocol.Position{Line: 21, Character: 5},
					End:   protocol.Position{Line: 21, Character: 11},
				},
			},
		},
	}
	sess := wireSession(t, fake)

	got, err := fakeTypeDefinition(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	want := []NavigationLocation{
		{
			TargetURI:            string(targetURI),
			TargetRange:          NavigationRange{StartLine: 20, StartColumn: 0, EndLine: 24, EndColumn: 1},
			TargetSelectionRange: NavigationRange{StartLine: 21, StartColumn: 5, EndLine: 21, EndColumn: 11},
		},
	}
	if diff := gocmp.Diff(want, got); diff != "" {
		t.Errorf("Lookup type definition mismatch (-want +got):\n%s", diff)
	}
}

func TestTypeDefinitionLookupNil(t *testing.T) {
	t.Parallel()

	sess := wireSession(t, &fakeServer{typeDefinitionSupported: true})
	got, err := fakeTypeDefinition(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Lookup returned %d type definitions, want 0: %+v", len(got), got)
	}
}

func TestTypeDefinitionLookupRequestParams(t *testing.T) {
	t.Parallel()

	fake := &fakeServer{typeDefinitionSupported: true}
	sess := wireSession(t, fake)
	pos := protocol.Position{Line: 9, Character: 17}
	if _, err := fakeTypeDefinition(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", pos); err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	calls := fake.typeDefinitionCalls()
	if len(calls) != 1 {
		t.Fatalf("expected exactly one type-definition request, got %d", len(calls))
	}
	if got := calls[0].TextDocument.URI; got != uri.File("/workspace/main.go") {
		t.Errorf("type-definition URI = %q, want %q", got, uri.File("/workspace/main.go"))
	}
	if calls[0].Position != pos {
		t.Errorf("type-definition position = %+v, want %+v", calls[0].Position, pos)
	}
}

func TestTypeDefinitionLookupSurfacesServerError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("server unavailable")
	sess := wireSession(t, &fakeServer{typeDefinitionSupported: true, typeDefinitionErr: sentinel})

	_, err := fakeTypeDefinition(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{})
	if err == nil {
		t.Fatal("Lookup returned nil error for a server failure")
	}
	got := err.Error()
	if !strings.Contains(got, "type definition request") || !strings.Contains(got, sentinel.Error()) {
		t.Fatalf("Lookup error = %v, want type definition request context and server error %q", err, sentinel)
	}
}
