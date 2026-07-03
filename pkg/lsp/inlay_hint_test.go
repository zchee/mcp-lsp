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
)

func (f *fakeServer) InlayHint(_ context.Context, params *protocol.InlayHintParams) ([]protocol.InlayHint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.inlayHintRequests = append(f.inlayHintRequests, *params)
	if f.inlayHintErr != nil {
		return nil, f.inlayHintErr
	}
	return f.inlayHintResult, nil
}

func (f *fakeServer) inlayHintCalls() []protocol.InlayHintParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.InlayHintParams(nil), f.inlayHintRequests...)
}

func fakeInlayHints(sess *serverSession) *InlayHints {
	mgr := &Manager{
		cfg:      map[string]ServerConfig{"go": {LanguageID: protocol.LanguageKindGo}},
		sessions: map[string]*serverSession{"go": sess},
		logger:   slog.New(slog.DiscardHandler),
	}
	return &InlayHints{mgr: mgr, timeout: 2 * time.Second}
}

func fullFileRange() protocol.Range {
	return protocol.Range{
		Start: protocol.Position{Line: 0, Character: 0},
		End:   protocol.Position{Line: 100, Character: 0},
	}
}

func TestInlayHintsLookupUnsupportedProvider(t *testing.T) {
	t.Parallel()

	fake := &fakeServer{}
	sess := wireSession(t, fake)
	if sess.capabilities.inlayHint {
		t.Fatal("session detected inlay-hint support that the fake did not advertise")
	}

	_, err := fakeInlayHints(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", fullFileRange())
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Lookup error = %v, want errors.Is ErrUnsupported", err)
	}
	if !strings.Contains(err.Error(), "inlay hint request") {
		t.Fatalf("Lookup error = %v, want the failing primitive named", err)
	}
	if got := len(fake.openedDocs()); got != 0 {
		t.Errorf("Lookup opened %d documents despite unsupported provider, want 0", got)
	}
	if got := len(fake.inlayHintCalls()); got != 0 {
		t.Errorf("Lookup issued %d requests despite unsupported provider, want 0", got)
	}
}

func TestInlayHintsLookupItems(t *testing.T) {
	t.Parallel()

	fake := &fakeServer{
		inlayHintSupported: true,
		inlayHintResult: []protocol.InlayHint{
			{
				Position: protocol.Position{Line: 3, Character: 12},
				Label:    protocol.String("string"),
				Kind:     protocol.InlayHintKindType,
			},
			{
				Position: protocol.Position{Line: 7, Character: 4},
				Label: protocol.InlayHintLabelPartSlice{
					{Value: "name:"},
					{Value: " "},
				},
				Kind: protocol.InlayHintKindParameter,
			},
			{
				Position: protocol.Position{Line: 9, Character: 0},
				Label:    protocol.String("unkinded"),
			},
		},
	}
	sess := wireSession(t, fake)
	if !sess.capabilities.inlayHint {
		t.Fatal("session did not detect inlay-hint support advertised by the fake")
	}

	got, err := fakeInlayHints(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", fullFileRange())
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	want := []InlayHintItem{
		{Line: 3, Column: 12, Label: "string", Kind: "type"},
		{Line: 7, Column: 4, Label: "name: ", Kind: "parameter"},
		{Line: 9, Column: 0, Label: "unkinded", Kind: ""},
	}
	if diff := gocmp.Diff(want, got); diff != "" {
		t.Errorf("Lookup inlay hints mismatch (-want +got):\n%s", diff)
	}
}

func TestInlayHintsLookupEmptyResult(t *testing.T) {
	t.Parallel()

	sess := wireSession(t, &fakeServer{inlayHintSupported: true})
	got, err := fakeInlayHints(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", fullFileRange())
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Lookup returned %d items, want 0: %+v", len(got), got)
	}
}

func TestInlayHintsLookupForwardsRange(t *testing.T) {
	t.Parallel()

	fake := &fakeServer{inlayHintSupported: true}
	sess := wireSession(t, fake)
	rng := protocol.Range{
		Start: protocol.Position{Line: 2, Character: 0},
		End:   protocol.Position{Line: 20, Character: 3},
	}
	if _, err := fakeInlayHints(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", rng); err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	calls := fake.inlayHintCalls()
	if len(calls) != 1 {
		t.Fatalf("expected exactly one inlay-hint request, got %d", len(calls))
	}
	if calls[0].Range != rng {
		t.Errorf("inlay-hint range = %+v, want %+v", calls[0].Range, rng)
	}
}

func TestInlayHintsLookupSurfacesServerError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("server unavailable")
	sess := wireSession(t, &fakeServer{inlayHintSupported: true, inlayHintErr: sentinel})

	_, err := fakeInlayHints(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", fullFileRange())
	if err == nil {
		t.Fatal("Lookup returned nil error for a server failure")
	}
	if !strings.Contains(err.Error(), "inlay hint request") || !strings.Contains(err.Error(), sentinel.Error()) {
		t.Fatalf("Lookup error = %v, want inlay hint request context and server error %q", err, sentinel)
	}
}
