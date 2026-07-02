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

package composite

import (
	"cmp"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

func refAt(uri string, line, col int, kind RefKind) Ref {
	return Ref{
		URI:   uri,
		Range: lsp.NavigationRange{StartLine: line, StartColumn: col, EndLine: line, EndColumn: col + 3},
		Kind:  kind,
	}
}

func TestSortRefsCanonicalOrder(t *testing.T) {
	t.Parallel()

	// Deliberately scrambled: differing URI, line, column, and kind. The two
	// entries at a.go 5:2 differ only in kind, so they exercise the final
	// tie-breaker: KindDefinition (2) sorts before KindDeclaration (3).
	in := []Ref{
		refAt("file:///b.go", 1, 0, KindReference),
		refAt("file:///a.go", 5, 2, KindDeclaration),
		refAt("file:///a.go", 5, 2, KindDefinition),
		refAt("file:///a.go", 5, 0, KindReference),
		refAt("file:///a.go", 1, 9, KindReference),
	}
	want := []Ref{
		refAt("file:///a.go", 1, 9, KindReference),
		refAt("file:///a.go", 5, 0, KindReference),
		refAt("file:///a.go", 5, 2, KindDefinition),
		refAt("file:///a.go", 5, 2, KindDeclaration),
		refAt("file:///b.go", 1, 0, KindReference),
	}
	if diff := gocmp.Diff(want, SortRefs(in)); diff != "" {
		t.Errorf("SortRefs mismatch (-want +got):\n%s", diff)
	}
}

func TestSortRefsIsDeterministic(t *testing.T) {
	t.Parallel()

	build := func() []Ref {
		return []Ref{
			refAt("file:///z.go", 3, 1, KindReference),
			refAt("file:///a.go", 1, 0, KindDefinition),
			refAt("file:///m.go", 2, 5, KindImplementation),
		}
	}
	first := SortRefs(build())
	second := SortRefs(build())
	if diff := gocmp.Diff(first, second); diff != "" {
		t.Errorf("SortRefs is not deterministic across runs (-first +second):\n%s", diff)
	}
}

func TestTruncate(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		items       []int
		maxKept     int
		wantKept    []int
		wantOmitted int
	}{
		"success: under the cap keeps everything sorted": {
			items:       []int{3, 1, 2},
			maxKept:     5,
			wantKept:    []int{1, 2, 3},
			wantOmitted: 0,
		},
		"success: at the cap keeps everything sorted": {
			items:       []int{3, 1, 2},
			maxKept:     3,
			wantKept:    []int{1, 2, 3},
			wantOmitted: 0,
		},
		"success: over the cap drops the sorted tail": {
			items:       []int{5, 3, 1, 4, 2},
			maxKept:     2,
			wantKept:    []int{1, 2},
			wantOmitted: 3,
		},
		"success: non-positive cap keeps everything sorted": {
			items:       []int{2, 1},
			maxKept:     0,
			wantKept:    []int{1, 2},
			wantOmitted: 0,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			kept, omitted := Truncate(tt.items, tt.maxKept, cmp.Compare[int])
			if omitted != tt.wantOmitted {
				t.Errorf("omitted = %d, want %d", omitted, tt.wantOmitted)
			}
			if diff := gocmp.Diff(tt.wantKept, kept); diff != "" {
				t.Errorf("kept mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTruncateIsDeterministic(t *testing.T) {
	t.Parallel()

	build := func() []int { return []int{9, 2, 7, 1, 5, 3} }
	firstKept, firstOmitted := Truncate(build(), 3, cmp.Compare[int])
	secondKept, secondOmitted := Truncate(build(), 3, cmp.Compare[int])
	if firstOmitted != secondOmitted {
		t.Fatalf("omitted differs across runs: %d vs %d", firstOmitted, secondOmitted)
	}
	if diff := gocmp.Diff(firstKept, secondKept); diff != "" {
		t.Errorf("Truncate is not deterministic (-first +second):\n%s", diff)
	}
}
