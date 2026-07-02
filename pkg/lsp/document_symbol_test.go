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

func (f *fakeServer) DocumentSymbol(_ context.Context, params *protocol.DocumentSymbolParams) (protocol.DocumentSymbolResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.documentSymbolRequests = append(f.documentSymbolRequests, *params)
	if f.documentSymbolErr != nil {
		return nil, f.documentSymbolErr
	}
	return f.documentSymbolResult, nil
}

func (f *fakeServer) documentSymbolCalls() []protocol.DocumentSymbolParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.DocumentSymbolParams(nil), f.documentSymbolRequests...)
}

func fakeDocumentSymbols(sess *serverSession) *DocumentSymbols {
	mgr := &Manager{
		cfg:      map[string]ServerConfig{"go": {LanguageID: protocol.LanguageKindGo}},
		sessions: map[string]*serverSession{"go": sess},
		logger:   slog.New(slog.DiscardHandler),
	}
	return &DocumentSymbols{mgr: mgr, timeout: 2 * time.Second}
}

func TestDocumentSymbolsLookupUnsupportedProvider(t *testing.T) {
	t.Parallel()

	fake := &fakeServer{}
	sess := wireSession(t, fake)
	if sess.capabilities.documentSymbol {
		t.Fatal("session detected document-symbol support that the fake did not advertise")
	}

	_, err := fakeDocumentSymbols(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Lookup error = %v, want errors.Is ErrUnsupported", err)
	}
	if !strings.Contains(err.Error(), "document symbol request") {
		t.Fatalf("Lookup error = %v, want the failing primitive named", err)
	}
	if got := len(fake.openedDocs()); got != 0 {
		t.Errorf("Lookup opened %d documents despite unsupported provider, want 0", got)
	}
	if got := len(fake.documentSymbolCalls()); got != 0 {
		t.Errorf("Lookup issued %d document-symbol requests despite unsupported provider, want 0", got)
	}
}

func TestDocumentSymbolsLookupHierarchical(t *testing.T) {
	t.Parallel()

	fake := &fakeServer{
		documentSymbolSupported: true,
		documentSymbolResult: protocol.DocumentSymbolSlice{
			{
				Name:   "Manager",
				Detail: new("struct{...}"),
				Kind:   protocol.SymbolKindStruct,
				Range: protocol.Range{
					Start: protocol.Position{Line: 10, Character: 0},
					End:   protocol.Position{Line: 30, Character: 1},
				},
				SelectionRange: protocol.Range{
					Start: protocol.Position{Line: 10, Character: 5},
					End:   protocol.Position{Line: 10, Character: 12},
				},
				Children: []protocol.DocumentSymbol{
					{
						Name: "Close",
						Kind: protocol.SymbolKindMethod,
						Range: protocol.Range{
							Start: protocol.Position{Line: 20, Character: 0},
							End:   protocol.Position{Line: 24, Character: 1},
						},
						SelectionRange: protocol.Range{
							Start: protocol.Position{Line: 20, Character: 18},
							End:   protocol.Position{Line: 20, Character: 23},
						},
					},
				},
			},
		},
	}
	sess := wireSession(t, fake)
	if !sess.capabilities.documentSymbol {
		t.Fatal("session did not detect document-symbol support advertised by the fake")
	}

	got, err := fakeDocumentSymbols(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	want := []DocumentSymbolEntry{
		{
			Name:           "Manager",
			Detail:         "struct{...}",
			Kind:           protocol.SymbolKindStruct,
			Range:          NavigationRange{StartLine: 10, StartColumn: 0, EndLine: 30, EndColumn: 1},
			SelectionRange: NavigationRange{StartLine: 10, StartColumn: 5, EndLine: 10, EndColumn: 12},
			Children: []DocumentSymbolEntry{
				{
					Name:           "Close",
					Kind:           protocol.SymbolKindMethod,
					Range:          NavigationRange{StartLine: 20, StartColumn: 0, EndLine: 24, EndColumn: 1},
					SelectionRange: NavigationRange{StartLine: 20, StartColumn: 18, EndLine: 20, EndColumn: 23},
				},
			},
		},
	}
	if diff := gocmp.Diff(want, got); diff != "" {
		t.Errorf("Lookup document symbols mismatch (-want +got):\n%s", diff)
	}
}

func TestDocumentSymbolsLookupSymbolInformationFallback(t *testing.T) {
	t.Parallel()

	symURI := uri.File("/workspace/main.go")
	fake := &fakeServer{
		documentSymbolSupported: true,
		documentSymbolResult: protocol.SymbolInformationSlice{
			{
				Name: "Manager",
				Kind: protocol.SymbolKindStruct,
				Location: protocol.Location{
					URI: symURI,
					Range: protocol.Range{
						Start: protocol.Position{Line: 10, Character: 0},
						End:   protocol.Position{Line: 30, Character: 1},
					},
				},
			},
			{
				Name: "Close",
				Kind: protocol.SymbolKindMethod,
				Location: protocol.Location{
					URI: symURI,
					Range: protocol.Range{
						Start: protocol.Position{Line: 20, Character: 0},
						End:   protocol.Position{Line: 24, Character: 1},
					},
				},
			},
		},
	}
	sess := wireSession(t, fake)

	got, err := fakeDocumentSymbols(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	managerRange := NavigationRange{StartLine: 10, StartColumn: 0, EndLine: 30, EndColumn: 1}
	closeRange := NavigationRange{StartLine: 20, StartColumn: 0, EndLine: 24, EndColumn: 1}
	want := []DocumentSymbolEntry{
		{Name: "Manager", Kind: protocol.SymbolKindStruct, Range: managerRange, SelectionRange: managerRange},
		{Name: "Close", Kind: protocol.SymbolKindMethod, Range: closeRange, SelectionRange: closeRange},
	}
	if diff := gocmp.Diff(want, got); diff != "" {
		t.Errorf("Lookup document symbols mismatch (-want +got):\n%s", diff)
	}
}

func TestDocumentSymbolsLookupNil(t *testing.T) {
	t.Parallel()

	sess := wireSession(t, &fakeServer{documentSymbolSupported: true})
	got, err := fakeDocumentSymbols(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Lookup returned %d symbols, want 0: %+v", len(got), got)
	}
}

func TestDocumentSymbolsLookupRequestParams(t *testing.T) {
	t.Parallel()

	fake := &fakeServer{documentSymbolSupported: true}
	sess := wireSession(t, fake)
	if _, err := fakeDocumentSymbols(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n"); err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	calls := fake.documentSymbolCalls()
	if len(calls) != 1 {
		t.Fatalf("expected exactly one document-symbol request, got %d", len(calls))
	}
	if got := calls[0].TextDocument.URI; got != uri.File("/workspace/main.go") {
		t.Errorf("document-symbol URI = %q, want %q", got, uri.File("/workspace/main.go"))
	}
}

func TestDocumentSymbolsLookupSurfacesServerError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("server unavailable")
	sess := wireSession(t, &fakeServer{documentSymbolSupported: true, documentSymbolErr: sentinel})

	_, err := fakeDocumentSymbols(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n")
	if err == nil {
		t.Fatal("Lookup returned nil error for a server failure")
	}
	got := err.Error()
	if !strings.Contains(got, "document symbol request") || !strings.Contains(got, sentinel.Error()) {
		t.Fatalf("Lookup error = %v, want document symbol request context and server error %q", err, sentinel)
	}
}
