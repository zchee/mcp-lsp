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

func (f *fakeServer) References(_ context.Context, params *protocol.ReferenceParams) ([]protocol.Location, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.referencesRequests = append(f.referencesRequests, *params)
	if f.referencesErr != nil {
		return nil, f.referencesErr
	}
	return f.referencesResult, nil
}

func (f *fakeServer) referencesCalls() []protocol.ReferenceParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.ReferenceParams(nil), f.referencesRequests...)
}

func fakeReferences(sess *serverSession) *References {
	mgr := &Manager{
		cfg:      map[string]ServerConfig{"go": {LanguageID: protocol.LanguageKindGo}},
		sessions: map[string]*serverSession{"go": sess},
		logger:   slog.New(slog.DiscardHandler),
	}
	return &References{mgr: mgr, timeout: 2 * time.Second}
}

func TestReferencesLookupUnsupportedProvider(t *testing.T) {
	t.Parallel()

	fake := &fakeServer{}
	sess := wireSession(t, fake)
	if sess.capabilities.references {
		t.Fatal("session detected references support that the fake did not advertise")
	}

	_, err := fakeReferences(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{}, true)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Lookup error = %v, want errors.Is ErrUnsupported", err)
	}
	if !strings.Contains(err.Error(), "references request") {
		t.Fatalf("Lookup error = %v, want the failing primitive named", err)
	}
	if got := len(fake.openedDocs()); got != 0 {
		t.Errorf("Lookup opened %d documents despite unsupported provider, want 0", got)
	}
	if got := len(fake.referencesCalls()); got != 0 {
		t.Errorf("Lookup issued %d references requests despite unsupported provider, want 0", got)
	}
}

func TestReferencesLookupLocations(t *testing.T) {
	t.Parallel()

	callerURI := uri.File("/workspace/caller.go")
	otherURI := uri.File("/workspace/other.go")
	fake := &fakeServer{
		referencesSupported: true,
		referencesResult: []protocol.Location{
			{
				URI: callerURI,
				Range: protocol.Range{
					Start: protocol.Position{Line: 4, Character: 8},
					End:   protocol.Position{Line: 4, Character: 15},
				},
			},
			{
				URI: otherURI,
				Range: protocol.Range{
					Start: protocol.Position{Line: 12, Character: 1},
					End:   protocol.Position{Line: 12, Character: 8},
				},
			},
		},
	}
	sess := wireSession(t, fake)
	if !sess.capabilities.references {
		t.Fatal("session did not detect references support advertised by the fake")
	}

	got, err := fakeReferences(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{Line: 2, Character: 6}, true)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	firstRange := NavigationRange{StartLine: 4, StartColumn: 8, EndLine: 4, EndColumn: 15}
	secondRange := NavigationRange{StartLine: 12, StartColumn: 1, EndLine: 12, EndColumn: 8}
	want := []NavigationLocation{
		{TargetURI: string(callerURI), TargetRange: firstRange, TargetSelectionRange: firstRange},
		{TargetURI: string(otherURI), TargetRange: secondRange, TargetSelectionRange: secondRange},
	}
	if diff := gocmp.Diff(want, got); diff != "" {
		t.Errorf("Lookup references mismatch (-want +got):\n%s", diff)
	}
}

func TestReferencesLookupEmptyResult(t *testing.T) {
	t.Parallel()

	sess := wireSession(t, &fakeServer{referencesSupported: true})
	got, err := fakeReferences(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{}, false)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Lookup returned %d references, want 0: %+v", len(got), got)
	}
}

func TestReferencesLookupRequestParams(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		includeDeclaration bool
	}{
		"success: include declaration is forwarded": {includeDeclaration: true},
		"success: exclude declaration is forwarded": {includeDeclaration: false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeServer{referencesSupported: true}
			sess := wireSession(t, fake)
			pos := protocol.Position{Line: 9, Character: 17}
			if _, err := fakeReferences(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", pos, tt.includeDeclaration); err != nil {
				t.Fatalf("Lookup: %v", err)
			}

			calls := fake.referencesCalls()
			if len(calls) != 1 {
				t.Fatalf("expected exactly one references request, got %d", len(calls))
			}
			if got := calls[0].TextDocument.URI; got != uri.File("/workspace/main.go") {
				t.Errorf("references URI = %q, want %q", got, uri.File("/workspace/main.go"))
			}
			if calls[0].Position != pos {
				t.Errorf("references position = %+v, want %+v", calls[0].Position, pos)
			}
			if calls[0].Context.IncludeDeclaration != tt.includeDeclaration {
				t.Errorf("references includeDeclaration = %v, want %v", calls[0].Context.IncludeDeclaration, tt.includeDeclaration)
			}
		})
	}
}

func TestReferencesLookupSurfacesServerError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("server unavailable")
	sess := wireSession(t, &fakeServer{referencesSupported: true, referencesErr: sentinel})

	_, err := fakeReferences(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{}, true)
	if err == nil {
		t.Fatal("Lookup returned nil error for a server failure")
	}
	got := err.Error()
	if !strings.Contains(got, "references request") || !strings.Contains(got, sentinel.Error()) {
		t.Fatalf("Lookup error = %v, want references request context and server error %q", err, sentinel)
	}
	if errors.Is(err, ErrUnsupported) {
		t.Fatalf("Lookup error = %v must not match ErrUnsupported for a server failure", err)
	}
}
