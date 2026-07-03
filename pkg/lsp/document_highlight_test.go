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

func (f *fakeServer) DocumentHighlight(_ context.Context, params *protocol.DocumentHighlightParams) ([]protocol.DocumentHighlight, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.documentHighlightRequests = append(f.documentHighlightRequests, *params)
	if f.documentHighlightErr != nil {
		return nil, f.documentHighlightErr
	}
	return f.documentHighlightResult, nil
}

func (f *fakeServer) documentHighlightCalls() []protocol.DocumentHighlightParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.DocumentHighlightParams(nil), f.documentHighlightRequests...)
}

func fakeDocumentHighlight(sess *serverSession) *DocumentHighlight {
	mgr := &Manager{
		cfg:      map[string]ServerConfig{"go": {LanguageID: protocol.LanguageKindGo}},
		sessions: map[string]*serverSession{"go": sess},
		logger:   slog.New(slog.DiscardHandler),
	}
	return &DocumentHighlight{mgr: mgr, timeout: 2 * time.Second}
}

func TestDocumentHighlightLookupUnsupportedProvider(t *testing.T) {
	t.Parallel()

	fake := &fakeServer{}
	sess := wireSession(t, fake)
	if sess.capabilities.documentHighlight {
		t.Fatal("session detected document-highlight support that the fake did not advertise")
	}

	_, err := fakeDocumentHighlight(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Lookup error = %v, want errors.Is ErrUnsupported", err)
	}
	if !strings.Contains(err.Error(), "document highlight request") {
		t.Fatalf("Lookup error = %v, want the failing primitive named", err)
	}
	if got := len(fake.openedDocs()); got != 0 {
		t.Errorf("Lookup opened %d documents despite unsupported provider, want 0", got)
	}
	if got := len(fake.documentHighlightCalls()); got != 0 {
		t.Errorf("Lookup issued %d requests despite unsupported provider, want 0", got)
	}
}

func TestDocumentHighlightLookupSpans(t *testing.T) {
	t.Parallel()

	fake := &fakeServer{
		documentHighlightSupported: true,
		documentHighlightResult: []protocol.DocumentHighlight{
			{
				Range: protocol.Range{
					Start: protocol.Position{Line: 4, Character: 8},
					End:   protocol.Position{Line: 4, Character: 15},
				},
				Kind: protocol.DocumentHighlightKindWrite,
			},
			{
				Range: protocol.Range{
					Start: protocol.Position{Line: 9, Character: 2},
					End:   protocol.Position{Line: 9, Character: 9},
				},
				Kind: protocol.DocumentHighlightKindRead,
			},
			{
				Range: protocol.Range{
					Start: protocol.Position{Line: 12, Character: 0},
					End:   protocol.Position{Line: 12, Character: 4},
				},
			},
		},
	}
	sess := wireSession(t, fake)
	if !sess.capabilities.documentHighlight {
		t.Fatal("session did not detect document-highlight support advertised by the fake")
	}

	got, err := fakeDocumentHighlight(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{Line: 4, Character: 10})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	want := []DocumentHighlightSpan{
		{Range: NavigationRange{StartLine: 4, StartColumn: 8, EndLine: 4, EndColumn: 15}, Kind: "write"},
		{Range: NavigationRange{StartLine: 9, StartColumn: 2, EndLine: 9, EndColumn: 9}, Kind: "read"},
		{Range: NavigationRange{StartLine: 12, StartColumn: 0, EndLine: 12, EndColumn: 4}, Kind: "text"},
	}
	if diff := gocmp.Diff(want, got); diff != "" {
		t.Errorf("Lookup highlight spans mismatch (-want +got):\n%s", diff)
	}
}

func TestDocumentHighlightLookupEmptyResult(t *testing.T) {
	t.Parallel()

	sess := wireSession(t, &fakeServer{documentHighlightSupported: true})
	got, err := fakeDocumentHighlight(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Lookup returned %d spans, want 0: %+v", len(got), got)
	}
}

func TestDocumentHighlightLookupSurfacesServerError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("server unavailable")
	sess := wireSession(t, &fakeServer{documentHighlightSupported: true, documentHighlightErr: sentinel})

	_, err := fakeDocumentHighlight(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{})
	if err == nil {
		t.Fatal("Lookup returned nil error for a server failure")
	}
	if !strings.Contains(err.Error(), "document highlight request") || !strings.Contains(err.Error(), sentinel.Error()) {
		t.Fatalf("Lookup error = %v, want document highlight request context and server error %q", err, sentinel)
	}
}
