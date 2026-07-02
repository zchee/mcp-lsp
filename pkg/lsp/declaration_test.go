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

func (f *fakeServer) Declaration(_ context.Context, params *protocol.DeclarationParams) (protocol.DeclarationResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.declarationRequests = append(f.declarationRequests, *params)
	if f.declarationErr != nil {
		return nil, f.declarationErr
	}
	return f.declarationResult, nil
}

func (f *fakeServer) declarationCalls() []protocol.DeclarationParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.DeclarationParams(nil), f.declarationRequests...)
}

func fakeDeclaration(sess *serverSession) *Declaration {
	mgr := &Manager{
		cfg:      map[string]ServerConfig{"go": {LanguageID: protocol.LanguageKindGo}},
		sessions: map[string]*serverSession{"go": sess},
		logger:   slog.New(slog.DiscardHandler),
	}
	return &Declaration{mgr: mgr, timeout: 2 * time.Second}
}

func TestDeclarationLookupUnsupportedProvider(t *testing.T) {
	t.Parallel()

	fake := &fakeServer{}
	sess := wireSession(t, fake)
	if sess.capabilities.declaration {
		t.Fatal("session detected declaration support that the fake did not advertise")
	}

	_, err := fakeDeclaration(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Lookup error = %v, want errors.Is ErrUnsupported", err)
	}
	if !strings.Contains(err.Error(), "declaration request") {
		t.Fatalf("Lookup error = %v, want the failing primitive named", err)
	}
	if got := len(fake.openedDocs()); got != 0 {
		t.Errorf("Lookup opened %d documents despite unsupported provider, want 0", got)
	}
	if got := len(fake.declarationCalls()); got != 0 {
		t.Errorf("Lookup issued %d declaration requests despite unsupported provider, want 0", got)
	}
}

func TestDeclarationLookupLocation(t *testing.T) {
	t.Parallel()

	targetURI := uri.File("/workspace/decl.go")
	fake := &fakeServer{
		declarationSupported: true,
		declarationResult: &protocol.Location{
			URI: targetURI,
			Range: protocol.Range{
				Start: protocol.Position{Line: 7, Character: 1},
				End:   protocol.Position{Line: 7, Character: 6},
			},
		},
	}
	sess := wireSession(t, fake)
	if !sess.capabilities.declaration {
		t.Fatal("session did not detect declaration support advertised by the fake")
	}

	got, err := fakeDeclaration(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{Line: 3, Character: 4})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	wantRange := NavigationRange{StartLine: 7, StartColumn: 1, EndLine: 7, EndColumn: 6}
	want := []NavigationLocation{
		{TargetURI: string(targetURI), TargetRange: wantRange, TargetSelectionRange: wantRange},
	}
	if diff := gocmp.Diff(want, got); diff != "" {
		t.Errorf("Lookup declaration mismatch (-want +got):\n%s", diff)
	}
}

func TestDeclarationLookupLocationSlice(t *testing.T) {
	t.Parallel()

	fooURI := uri.File("/workspace/foo_decl.go")
	barURI := uri.File("/workspace/bar_decl.go")
	fake := &fakeServer{
		declarationSupported: true,
		declarationResult: protocol.LocationSlice{
			{
				URI: fooURI,
				Range: protocol.Range{
					Start: protocol.Position{Line: 1, Character: 2},
					End:   protocol.Position{Line: 1, Character: 5},
				},
			},
			{
				URI: barURI,
				Range: protocol.Range{
					Start: protocol.Position{Line: 8, Character: 13},
					End:   protocol.Position{Line: 8, Character: 21},
				},
			},
		},
	}
	sess := wireSession(t, fake)

	got, err := fakeDeclaration(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	firstRange := NavigationRange{StartLine: 1, StartColumn: 2, EndLine: 1, EndColumn: 5}
	secondRange := NavigationRange{StartLine: 8, StartColumn: 13, EndLine: 8, EndColumn: 21}
	want := []NavigationLocation{
		{TargetURI: string(fooURI), TargetRange: firstRange, TargetSelectionRange: firstRange},
		{TargetURI: string(barURI), TargetRange: secondRange, TargetSelectionRange: secondRange},
	}
	if diff := gocmp.Diff(want, got); diff != "" {
		t.Errorf("Lookup declaration mismatch (-want +got):\n%s", diff)
	}
}

func TestDeclarationLookupDeclarationLinkSlice(t *testing.T) {
	t.Parallel()

	targetURI := uri.File("/workspace/linked_decl.go")
	originRange := protocol.Range{
		Start: protocol.Position{Line: 3, Character: 4},
		End:   protocol.Position{Line: 3, Character: 10},
	}
	fake := &fakeServer{
		declarationSupported: true,
		declarationResult: protocol.DeclarationLinkSlice{
			{
				OriginSelectionRange: &originRange,
				TargetURI:            targetURI,
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

	got, err := fakeDeclaration(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	origin := NavigationRange{StartLine: 3, StartColumn: 4, EndLine: 3, EndColumn: 10}
	want := []NavigationLocation{
		{
			TargetURI:            string(targetURI),
			TargetRange:          NavigationRange{StartLine: 20, StartColumn: 0, EndLine: 24, EndColumn: 1},
			TargetSelectionRange: NavigationRange{StartLine: 21, StartColumn: 5, EndLine: 21, EndColumn: 11},
			OriginSelectionRange: &origin,
		},
	}
	if diff := gocmp.Diff(want, got); diff != "" {
		t.Errorf("Lookup declaration mismatch (-want +got):\n%s", diff)
	}
}

func TestDeclarationLookupNil(t *testing.T) {
	t.Parallel()

	sess := wireSession(t, &fakeServer{declarationSupported: true})
	got, err := fakeDeclaration(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Lookup returned %d declarations, want 0: %+v", len(got), got)
	}
}

func TestDeclarationLookupSurfacesServerError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("server unavailable")
	sess := wireSession(t, &fakeServer{declarationSupported: true, declarationErr: sentinel})

	_, err := fakeDeclaration(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{})
	if err == nil {
		t.Fatal("Lookup returned nil error for a server failure")
	}
	got := err.Error()
	if !strings.Contains(got, "declaration request") || !strings.Contains(got, sentinel.Error()) {
		t.Fatalf("Lookup error = %v, want declaration request context and server error %q", err, sentinel)
	}
}
