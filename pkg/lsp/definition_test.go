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

	"github.com/google/go-cmp/cmp"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func (f *fakeServer) Definition(_ context.Context, params *protocol.DefinitionParams) (protocol.DefinitionResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.definitionRequests = append(f.definitionRequests, *params)
	if f.definitionErr != nil {
		return nil, f.definitionErr
	}
	return f.definitionResult, nil
}

func (f *fakeServer) definitionCalls() []protocol.DefinitionParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.DefinitionParams(nil), f.definitionRequests...)
}

func fakeDefinition(sess *serverSession) *Definition {
	mgr := &Manager{
		cfg:      map[string]ServerConfig{"go": {LanguageID: protocol.LanguageKindGo}},
		sessions: map[string]*serverSession{"go": sess},
		logger:   slog.New(slog.DiscardHandler),
	}
	return &Definition{mgr: mgr, timeout: 2 * time.Second}
}

func TestDefinitionLookupLocation(t *testing.T) {
	t.Parallel()

	targetURI := uri.File("/workspace/lib.go")
	targetRange := protocol.Range{
		Start: protocol.Position{Line: 7, Character: 1},
		End:   protocol.Position{Line: 7, Character: 6},
	}
	fake := &fakeServer{
		definitionResult: &protocol.Location{
			URI:   targetURI,
			Range: targetRange,
		},
	}
	sess := wireSession(t, fake)

	defs := fakeDefinition(sess)
	got, err := defs.Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{Line: 3, Character: 4})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	wantRange := DefinitionRange{StartLine: 7, StartColumn: 1, EndLine: 7, EndColumn: 6}
	want := []DefinitionLocation{
		{
			TargetURI:            string(targetURI),
			TargetRange:          wantRange,
			TargetSelectionRange: wantRange,
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Lookup definition mismatch (-want +got):\n%s", diff)
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

func TestDefinitionLookupLocationSlice(t *testing.T) {
	t.Parallel()

	fooURI := uri.File("/workspace/foo.go")
	barURI := uri.File("/workspace/bar.go")
	fake := &fakeServer{
		definitionResult: protocol.LocationSlice{
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

	got, err := fakeDefinition(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	firstRange := DefinitionRange{StartLine: 1, StartColumn: 2, EndLine: 1, EndColumn: 5}
	secondRange := DefinitionRange{StartLine: 8, StartColumn: 13, EndLine: 8, EndColumn: 21}
	want := []DefinitionLocation{
		{TargetURI: string(fooURI), TargetRange: firstRange, TargetSelectionRange: firstRange},
		{TargetURI: string(barURI), TargetRange: secondRange, TargetSelectionRange: secondRange},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Lookup definition mismatch (-want +got):\n%s", diff)
	}
}

func TestDefinitionLookupDefinitionLinkSlice(t *testing.T) {
	t.Parallel()

	targetURI := uri.File("/workspace/linked.go")
	originRange := protocol.Range{
		Start: protocol.Position{Line: 3, Character: 4},
		End:   protocol.Position{Line: 3, Character: 10},
	}
	fake := &fakeServer{
		definitionResult: protocol.DefinitionLinkSlice{
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

	got, err := fakeDefinition(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	origin := DefinitionRange{StartLine: 3, StartColumn: 4, EndLine: 3, EndColumn: 10}
	want := []DefinitionLocation{
		{
			TargetURI:            string(targetURI),
			TargetRange:          DefinitionRange{StartLine: 20, StartColumn: 0, EndLine: 24, EndColumn: 1},
			TargetSelectionRange: DefinitionRange{StartLine: 21, StartColumn: 5, EndLine: 21, EndColumn: 11},
			OriginSelectionRange: &origin,
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Lookup definition mismatch (-want +got):\n%s", diff)
	}
}

func TestDefinitionLookupNil(t *testing.T) {
	t.Parallel()

	sess := wireSession(t, &fakeServer{})
	got, err := fakeDefinition(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{})
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Lookup returned %d definitions, want 0: %+v", len(got), got)
	}
}

func TestDefinitionLookupRequestParams(t *testing.T) {
	t.Parallel()

	fake := &fakeServer{}
	sess := wireSession(t, fake)
	pos := protocol.Position{Line: 9, Character: 17}
	if _, err := fakeDefinition(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", pos); err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	calls := fake.definitionCalls()
	if len(calls) != 1 {
		t.Fatalf("expected exactly one definition request, got %d", len(calls))
	}
	if got := calls[0].TextDocument.URI; got != uri.File("/workspace/main.go") {
		t.Errorf("definition URI = %q, want %q", got, uri.File("/workspace/main.go"))
	}
	if calls[0].Position != pos {
		t.Errorf("definition position = %+v, want %+v", calls[0].Position, pos)
	}
}

func TestDefinitionLookupSurfacesServerError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("server unavailable")
	fake := &fakeServer{definitionErr: sentinel}
	sess := wireSession(t, fake)

	_, err := fakeDefinition(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n", protocol.Position{})
	if err == nil {
		t.Fatal("Lookup returned nil error for a server failure")
	}
	got := err.Error()
	if !strings.Contains(got, "definition request") || !strings.Contains(got, sentinel.Error()) {
		t.Fatalf("Lookup error = %v, want definition request context and server error %q", err, sentinel)
	}
}

func TestInitializeParamsAdvertisesDefinitionLinkSupport(t *testing.T) {
	t.Parallel()

	params := initializeParams(uri.File("/workspace"))
	if params.Capabilities.TextDocument == nil {
		t.Fatal("TextDocument capabilities are nil")
	}
	def := params.Capabilities.TextDocument.Definition
	if def == nil {
		t.Fatal("Definition capabilities are nil")
	}
	if def.LinkSupport == nil {
		t.Fatal("Definition LinkSupport is nil")
	}
	if !*def.LinkSupport {
		t.Fatal("Definition LinkSupport = false, want true")
	}
}
