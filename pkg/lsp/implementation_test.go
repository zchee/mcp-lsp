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

func (f *fakeServer) Implementation(_ context.Context, params *protocol.ImplementationParams) (protocol.DefinitionResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.implementationRequests = append(f.implementationRequests, *params)
	if f.implementationErr != nil {
		return nil, f.implementationErr
	}
	return f.implementationResult, nil
}

func (f *fakeServer) implementationCalls() []protocol.ImplementationParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.ImplementationParams(nil), f.implementationRequests...)
}

func fakeImplementation(sess *serverSession) *Implementation {
	mgr := &Manager{
		cfg:      map[string]ServerConfig{"go": {LanguageID: protocol.LanguageKindGo}},
		sessions: map[string]*serverSession{"go": sess},
		logger:   slog.New(slog.DiscardHandler),
	}
	return &Implementation{mgr: mgr, timeout: 2 * time.Second}
}

func TestImplementationProviderSupported(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider protocol.ImplementationProvider
		want     bool
	}{
		{name: "nil", provider: nil, want: false},
		{name: "boolean false", provider: protocol.Boolean(false), want: false},
		{name: "boolean true", provider: protocol.Boolean(true), want: true},
		{name: "options", provider: &protocol.ImplementationOptions{}, want: true},
		{name: "registration options", provider: &protocol.ImplementationRegistrationOptions{}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := implementationProviderSupported(tt.provider); got != tt.want {
				t.Errorf("implementationProviderSupported(%T) = %v, want %v", tt.provider, got, tt.want)
			}
		})
	}
}

func TestInitializeParamsAdvertisesImplementationLinkSupport(t *testing.T) {
	t.Parallel()

	params := initializeParams(uri.File("/workspace"))
	if params.Capabilities.TextDocument == nil {
		t.Fatal("TextDocument capabilities are nil")
	}
	impl := params.Capabilities.TextDocument.Implementation
	if impl == nil {
		t.Fatal("Implementation capabilities are nil")
	}
	if impl.LinkSupport == nil {
		t.Fatal("Implementation LinkSupport is nil")
	}
	if !*impl.LinkSupport {
		t.Fatal("Implementation LinkSupport = false, want true")
	}
}

func TestImplementationLookupUnsupportedProvider(t *testing.T) {
	t.Parallel()

	fake := &fakeServer{}
	sess := wireSession(t, fake)
	if sess.implementationSupported {
		t.Fatal("session detected implementation support that the fake did not advertise")
	}

	_, err := fakeImplementation(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Lookup error = %v, want errors.Is ErrUnsupported", err)
	}
	if !strings.Contains(err.Error(), "implementation request") {
		t.Fatalf("Lookup error = %v, want the failing primitive named", err)
	}
	if got := len(fake.openedDocs()); got != 0 {
		t.Errorf("Lookup opened %d documents despite unsupported provider, want 0", got)
	}
	if got := len(fake.implementationCalls()); got != 0 {
		t.Errorf("Lookup issued %d implementation requests despite unsupported provider, want 0", got)
	}
}

func TestImplementationLookupLocation(t *testing.T) {
	t.Parallel()

	targetURI := uri.File("/workspace/impl.go")
	targetRange := protocol.Range{
		Start: protocol.Position{Line: 7, Character: 1},
		End:   protocol.Position{Line: 7, Character: 6},
	}
	fake := &fakeServer{
		implementationSupported: true,
		implementationResult: &protocol.Location{
			URI:   targetURI,
			Range: targetRange,
		},
	}
	sess := wireSession(t, fake)
	if !sess.implementationSupported {
		t.Fatal("session did not detect implementation support advertised by the fake")
	}

	impls := fakeImplementation(sess)
	got, err := impls.Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{Line: 3, Character: 4})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	wantRange := NavigationRange{
		StartLine:   7,
		StartColumn: 1,
		EndLine:     7,
		EndColumn:   6,
	}
	want := []NavigationLocation{
		{
			TargetURI:            string(targetURI),
			TargetRange:          wantRange,
			TargetSelectionRange: wantRange,
		},
	}
	if diff := gocmp.Diff(want, got); diff != "" {
		t.Errorf("Lookup implementation mismatch (-want +got):\n%s", diff)
	}

	opened := fake.openedDocs()
	if len(opened) != 1 {
		t.Fatalf("expected exactly one didOpen, got %d", len(opened))
	}
	if opened[0].TextDocument.Text != "package main\n" {
		t.Errorf("didOpen text = %q, want %q", opened[0].TextDocument.Text, "package main\n")
	}
	if opened[0].TextDocument.LanguageID != protocol.LanguageKindGo {
		t.Errorf("didOpen languageID = %q, want %q", opened[0].TextDocument.LanguageID, protocol.LanguageKindGo)
	}
}

func TestImplementationLookupLocationSlice(t *testing.T) {
	t.Parallel()

	fooURI := uri.File("/workspace/foo_impl.go")
	barURI := uri.File("/workspace/bar_impl.go")
	fake := &fakeServer{
		implementationSupported: true,
		implementationResult: protocol.LocationSlice{
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

	got, err := fakeImplementation(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	firstRange := NavigationRange{
		StartLine:   1,
		StartColumn: 2,
		EndLine:     1,
		EndColumn:   5,
	}
	secondRange := NavigationRange{
		StartLine:   8,
		StartColumn: 13,
		EndLine:     8,
		EndColumn:   21,
	}
	want := []NavigationLocation{
		{
			TargetURI: string(fooURI), TargetRange: firstRange, TargetSelectionRange: firstRange,
		},
		{
			TargetURI: string(barURI), TargetRange: secondRange, TargetSelectionRange: secondRange,
		},
	}
	if diff := gocmp.Diff(want, got); diff != "" {
		t.Errorf("Lookup implementation mismatch (-want +got):\n%s", diff)
	}
}

func TestImplementationLookupDefinitionLinkSlice(t *testing.T) {
	t.Parallel()

	targetURI := uri.File("/workspace/linked_impl.go")
	originRange := protocol.Range{
		Start: protocol.Position{Line: 3, Character: 4},
		End:   protocol.Position{Line: 3, Character: 10},
	}
	fake := &fakeServer{
		implementationSupported: true,
		implementationResult: protocol.DefinitionLinkSlice{
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

	got, err := fakeImplementation(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	origin := NavigationRange{
		StartLine:   3,
		StartColumn: 4,
		EndLine:     3,
		EndColumn:   10,
	}
	want := []NavigationLocation{
		{
			TargetURI: string(targetURI),
			TargetRange: NavigationRange{
				StartLine:   20,
				StartColumn: 0,
				EndLine:     24,
				EndColumn:   1,
			},
			TargetSelectionRange: NavigationRange{
				StartLine:   21,
				StartColumn: 5,
				EndLine:     21,
				EndColumn:   11,
			},
			OriginSelectionRange: &origin,
		},
	}
	if diff := gocmp.Diff(want, got); diff != "" {
		t.Errorf("Lookup implementation mismatch (-want +got):\n%s", diff)
	}
}

func TestImplementationLookupNil(t *testing.T) {
	t.Parallel()

	sess := wireSession(t, &fakeServer{implementationSupported: true})
	got, err := fakeImplementation(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Lookup returned %d implementations, want 0: %+v", len(got), got)
	}
}

func TestImplementationLookupRequestParams(t *testing.T) {
	t.Parallel()

	fake := &fakeServer{implementationSupported: true}
	sess := wireSession(t, fake)
	pos := protocol.Position{Line: 9, Character: 17}
	if _, err := fakeImplementation(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", pos); err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	calls := fake.implementationCalls()
	if len(calls) != 1 {
		t.Fatalf("expected exactly one implementation request, got %d", len(calls))
	}
	if got := calls[0].TextDocument.URI; got != uri.File("/workspace/main.go") {
		t.Errorf("implementation URI = %q, want %q", got, uri.File("/workspace/main.go"))
	}
	if calls[0].Position != pos {
		t.Errorf("implementation position = %+v, want %+v", calls[0].Position, pos)
	}
}

func TestImplementationLookupSurfacesServerError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("server unavailable")
	fake := &fakeServer{implementationSupported: true, implementationErr: sentinel}
	sess := wireSession(t, fake)

	_, err := fakeImplementation(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{})
	if err == nil {
		t.Fatal("Lookup returned nil error for a server failure")
	}
	got := err.Error()
	if !strings.Contains(got, "implementation request") || !strings.Contains(got, sentinel.Error()) {
		t.Fatalf("Lookup error = %v, want implementation request context and server error %q", err, sentinel)
	}
}
