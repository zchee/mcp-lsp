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
	"slices"
)

// refOrder is the canonical ordering for a [Ref]: by URI, then range start
// line and column, then kind. Truncation sorts by this order before capping so
// two runs over the same set drop the same tail.
func refOrder(a, b Ref) int {
	if c := cmp.Compare(a.URI, b.URI); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Range.StartLine, b.Range.StartLine); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Range.StartColumn, b.Range.StartColumn); c != 0 {
		return c
	}
	return cmp.Compare(a.Kind, b.Kind)
}

// SortRefs orders refs canonically in place and returns them, so reference
// collections are deterministic before dedup, truncation, or output.
func SortRefs(refs []Ref) []Ref {
	slices.SortFunc(refs, refOrder)
	return refs
}

// Truncate sorts items by less and returns at most max of them plus the number
// dropped. Sorting before capping makes the retained subset deterministic
// across runs even when the caller assembled items in a nondeterministic
// order. A non-positive max keeps everything.
func Truncate[T any](items []T, maxKept int, less func(a, b T) int) (kept []T, omitted int) {
	slices.SortFunc(items, less)
	if maxKept <= 0 || len(items) <= maxKept {
		return items, 0
	}
	return items[:maxKept], len(items) - maxKept
}
