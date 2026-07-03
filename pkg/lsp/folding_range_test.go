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
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	gocmp "github.com/google/go-cmp/cmp"
	"go.lsp.dev/protocol"
)

func (f *fakeServer) FoldingRanges(_ context.Context, params *protocol.FoldingRangeParams) ([]protocol.FoldingRange, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.foldingRangeRequests = append(f.foldingRangeRequests, *params)
	if f.foldingRangeErr != nil {
		return nil, f.foldingRangeErr
	}
	return f.foldingRangeResult, nil
}

func (f *fakeServer) foldingRangeCalls() []protocol.FoldingRangeParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.FoldingRangeParams(nil), f.foldingRangeRequests...)
}

func fakeFoldingRanges(sess *serverSession) *FoldingRanges {
	mgr := &Manager{
		cfg:      map[string]ServerConfig{"go": {LanguageID: protocol.LanguageKindGo}},
		sessions: map[string]*serverSession{"go": sess},
		logger:   slog.New(slog.DiscardHandler),
	}
	return &FoldingRanges{mgr: mgr, timeout: 2 * time.Second}
}

func uint32Ptr(v uint32) *uint32 { return &v }

func intPtr(v int) *int { return &v }

func strPtr(v string) *string { return &v }

func TestFoldingRangesLookupUnsupportedProvider(t *testing.T) {
	t.Parallel()

	fake := &fakeServer{}
	sess := wireSession(t, fake)
	if sess.capabilities.foldingRange {
		t.Fatal("session detected folding-range support that the fake did not advertise")
	}

	_, err := fakeFoldingRanges(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Lookup error = %v, want errors.Is ErrUnsupported", err)
	}
	if !strings.Contains(err.Error(), "folding range request") {
		t.Fatalf("Lookup error = %v, want the failing primitive named", err)
	}
	if got := len(fake.openedDocs()); got != 0 {
		t.Errorf("Lookup opened %d documents despite unsupported provider, want 0", got)
	}
	if got := len(fake.foldingRangeCalls()); got != 0 {
		t.Errorf("Lookup issued %d requests despite unsupported provider, want 0", got)
	}
}

func TestFoldingRangesLookupServerError(t *testing.T) {
	t.Parallel()

	fake := &fakeServer{
		foldingRangeSupported: true,
		foldingRangeErr:       fmt.Errorf("boom"),
	}
	sess := wireSession(t, fake)

	_, err := fakeFoldingRanges(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n")
	if err == nil {
		t.Fatal("Lookup error = nil, want the server error surfaced")
	}
	if errors.Is(err, ErrUnsupported) {
		t.Fatalf("Lookup error = %v must not match ErrUnsupported for a server failure", err)
	}
	if !strings.Contains(err.Error(), "folding range request") {
		t.Fatalf("Lookup error = %v, want the failing primitive named", err)
	}
}

func TestFoldingRangesLookupFlattens(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		raw  []protocol.FoldingRange
		want []FoldingRangeItem
	}{
		"line-level folds with kinds": {
			raw: []protocol.FoldingRange{
				{StartLine: 2, EndLine: 8, Kind: protocol.FoldingRangeKindComment},
				{StartLine: 0, EndLine: 1, Kind: protocol.FoldingRangeKindImports},
			},
			want: []FoldingRangeItem{
				{StartLine: 2, EndLine: 8, Kind: "comment"},
				{StartLine: 0, EndLine: 1, Kind: "imports"},
			},
		},
		"character-precise fold with collapsed text": {
			raw: []protocol.FoldingRange{
				{
					StartLine:      3,
					StartCharacter: uint32Ptr(4),
					EndLine:        10,
					EndCharacter:   uint32Ptr(1),
					Kind:           protocol.FoldingRangeKindRegion,
					CollapsedText:  strPtr("…"),
				},
			},
			want: []FoldingRangeItem{
				{
					StartLine:     3,
					StartColumn:   intPtr(4),
					EndLine:       10,
					EndColumn:     intPtr(1),
					Kind:          "region",
					CollapsedText: "…",
				},
			},
		},
		"kindless fold keeps empty kind": {
			raw: []protocol.FoldingRange{
				{StartLine: 5, EndLine: 6},
			},
			want: []FoldingRangeItem{
				{StartLine: 5, EndLine: 6},
			},
		},
		"empty result": {
			raw:  nil,
			want: []FoldingRangeItem{},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeServer{foldingRangeSupported: true, foldingRangeResult: tt.raw}
			sess := wireSession(t, fake)
			if !sess.capabilities.foldingRange {
				t.Fatal("session did not detect folding-range support advertised by the fake")
			}

			got, err := fakeFoldingRanges(sess).Lookup(t.Context(), "go", "/workspace/main.go", "package main\n")
			if err != nil {
				t.Fatalf("Lookup: %v", err)
			}
			if diff := gocmp.Diff(tt.want, got); diff != "" {
				t.Errorf("FoldingRangeItem mismatch (-want +got):\n%s", diff)
			}
			if got := len(fake.foldingRangeCalls()); got != 1 {
				t.Errorf("Lookup issued %d folding-range requests, want 1", got)
			}
		})
	}
}
