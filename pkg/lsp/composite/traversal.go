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

// Package composite orchestrates multiple language-server requests into single
// agent-facing tools. It sits between pkg/lsp (zero-based protocol/wire) and
// pkg/mcp (one-based agent DTOs): it consumes only the exported *lsp.Manager
// helper surface and keeps every range zero-based, leaving the one-based
// conversion to the MCP handlers. This file holds the bounded-traversal
// primitives shared by graph-shaped composites.
package composite

import (
	"fmt"
	"time"

	"go.lsp.dev/protocol"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

// Budget is an immutable per-call bound on a composite's fan-out. Its timing
// fields describe the two-phase model composites run: the epicenter leg's
// readiness gate runs inside ReadinessEnvelope, then the concurrent fan-out of
// the remaining legs runs inside Deadline, which starts once the epicenter
// resolves. The traversal primitives in this file consume only the node,
// reference, and file caps; the timing fields are wired by the composites.
type Budget struct {
	MaxDepth          int           // call/type hierarchy traversal depth
	MaxNodes          int           // total hierarchy nodes visited
	MaxReferences     int           // reference/location occurrences collected
	MaxFiles          int           // files opened for reads
	MaxDiagFiles      int           // files gathered for diagnostics (not MaxFiles)
	MaxInflight       int           // concurrent language-server requests per session
	ReadinessAttempts int           // stable-lookup attempts for the epicenter
	ReadinessDelay    time.Duration // delay between readiness attempts
	ReadinessEnvelope time.Duration // epicenter readiness phase budget
	Deadline          time.Duration // fan-out phase budget, starting after readiness
}

// DefaultBudget returns the documented default bounds. Callers copy and adjust
// individual fields as needed; the value is safe to share because Budget is
// used read-only.
func DefaultBudget() Budget {
	return Budget{
		MaxDepth:          2,
		MaxNodes:          80,
		MaxReferences:     500,
		MaxFiles:          40,
		MaxDiagFiles:      5,
		MaxInflight:       6,
		ReadinessAttempts: 10,
		ReadinessDelay:    250 * time.Millisecond,
		ReadinessEnvelope: 15 * time.Second,
		Deadline:          5 * time.Second,
	}
}

// RefKind classifies what a [Ref] points at, which decides both how it is
// deduplicated and whether it counts against the node or reference budget.
type RefKind uint8

// Reference-graph node and location kinds.
const (
	// KindReference is a plain reference occurrence.
	KindReference RefKind = iota + 1
	// KindDefinition is a definition target.
	KindDefinition
	// KindDeclaration is a declaration target.
	KindDeclaration
	// KindTypeDefinition is a type-definition target.
	KindTypeDefinition
	// KindImplementation is an implementation target.
	KindImplementation
	// KindIncomingCall is a caller node in the call hierarchy.
	KindIncomingCall
	// KindOutgoingCall is a callee node in the call hierarchy.
	KindOutgoingCall
	// KindSuperType is a supertype node in the type hierarchy.
	KindSuperType
	// KindSubType is a subtype node in the type hierarchy.
	KindSubType
)

// String returns the lower-camel name of the kind.
func (k RefKind) String() string {
	switch k {
	case KindReference:
		return "reference"
	case KindDefinition:
		return "definition"
	case KindDeclaration:
		return "declaration"
	case KindTypeDefinition:
		return "typeDefinition"
	case KindImplementation:
		return "implementation"
	case KindIncomingCall:
		return "incomingCall"
	case KindOutgoingCall:
		return "outgoingCall"
	case KindSuperType:
		return "superType"
	case KindSubType:
		return "subType"
	default:
		return "unknown"
	}
}

// isHierarchy reports whether the kind is a call/type-hierarchy node. Hierarchy
// kinds are deduplicated by symbol identity and counted against MaxNodes;
// location kinds are deduplicated by occurrence and counted against
// MaxReferences.
func (k RefKind) isHierarchy() bool {
	switch k {
	case KindIncomingCall, KindOutgoingCall, KindSuperType, KindSubType:
		return true
	default:
		return false
	}
}

// Ref identifies a graph element with zero-based positions mirroring
// [lsp.NavigationRange]. Range is the occurrence anchor used for location
// dedup; SelectionStart is the symbol anchor used for hierarchy dedup and is
// the zero value for location kinds that carry no selection range.
type Ref struct {
	URI            string
	Range          lsp.NavigationRange
	SelectionStart protocol.Position
	Kind           RefKind
}

// symbolKey identifies a hierarchy node by the symbol it names, so recursion
// and repeated edges collapse to one visit.
type symbolKey struct {
	uri  string
	line uint32
	char uint32
	kind RefKind
}

// occurrenceKey identifies a location by where it occurs, so the same
// reference reported twice collapses to one visit.
type occurrenceKey struct {
	uri         string
	startLine   int
	startColumn int
	endLine     int
	endColumn   int
	kind        RefKind
}

// Traversal enforces deduplication, cycle breaking, and the node/reference
// budget for one composite invocation. It is not safe for concurrent use; the
// fan-out that mutates it must serialize Visit calls.
type Traversal struct {
	maxNodes    int
	maxRefs     int
	maxFiles    int
	visitedSyms map[symbolKey]struct{}
	visitedOccs map[occurrenceKey]struct{}
	nodes       int
	refs        int
	files       int
}

// NewTraversal returns a traversal bounded by budget. Only budget's node,
// reference, and file caps are retained; the timing fields belong to the
// composites that drive the fan-out.
func NewTraversal(budget *Budget) *Traversal {
	return &Traversal{
		maxNodes:    budget.MaxNodes,
		maxRefs:     budget.MaxReferences,
		maxFiles:    budget.MaxFiles,
		visitedSyms: make(map[symbolKey]struct{}),
		visitedOccs: make(map[occurrenceKey]struct{}),
	}
}

// Visit records r and reports whether this is its first sighting. Hierarchy
// kinds are keyed by symbol and count against MaxNodes; location kinds are
// keyed by occurrence and count against MaxReferences. A repeat sighting
// returns firstTime=false with no error and no budget change, which is how
// cycles terminate. The first sighting that pushes a counter past its cap
// returns a non-nil error so the caller can stop and mark the result
// truncated.
func (t *Traversal) Visit(r Ref) (firstTime bool, err error) {
	if r.Kind.isHierarchy() {
		key := symbolKey{uri: r.URI, line: r.SelectionStart.Line, char: r.SelectionStart.Character, kind: r.Kind}
		if _, seen := t.visitedSyms[key]; seen {
			return false, nil
		}
		t.visitedSyms[key] = struct{}{}
		t.nodes++
		if t.nodes > t.maxNodes {
			return true, fmt.Errorf("traversal exceeded MaxNodes=%d", t.maxNodes)
		}
		return true, nil
	}

	key := occurrenceKey{
		uri:         r.URI,
		startLine:   r.Range.StartLine,
		startColumn: r.Range.StartColumn,
		endLine:     r.Range.EndLine,
		endColumn:   r.Range.EndColumn,
		kind:        r.Kind,
	}
	if _, seen := t.visitedOccs[key]; seen {
		return false, nil
	}
	t.visitedOccs[key] = struct{}{}
	t.refs++
	if t.refs > t.maxRefs {
		return true, fmt.Errorf("traversal exceeded MaxReferences=%d", t.maxRefs)
	}
	return true, nil
}

// AddFile records that a distinct file was opened for reads and reports whether
// the file budget is still within MaxFiles. It returns a non-nil error once the
// cap is exceeded so the caller can stop opening further files.
func (t *Traversal) AddFile() error {
	t.files++
	if t.files > t.maxFiles {
		return fmt.Errorf("traversal exceeded MaxFiles=%d", t.maxFiles)
	}
	return nil
}

// Nodes reports the number of distinct hierarchy nodes visited so far.
func (t *Traversal) Nodes() int { return t.nodes }

// Refs reports the number of distinct location occurrences visited so far.
func (t *Traversal) Refs() int { return t.refs }
