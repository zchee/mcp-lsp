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
	"testing"
	"time"

	"go.lsp.dev/protocol"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

func TestDefaultBudget(t *testing.T) {
	t.Parallel()

	got := DefaultBudget()
	want := Budget{
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
	if got != want {
		t.Errorf("DefaultBudget() = %+v, want %+v", got, want)
	}
}

func TestRefKindString(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		kind RefKind
		want string
	}{
		"success: reference":      {kind: KindReference, want: "reference"},
		"success: definition":     {kind: KindDefinition, want: "definition"},
		"success: declaration":    {kind: KindDeclaration, want: "declaration"},
		"success: typeDefinition": {kind: KindTypeDefinition, want: "typeDefinition"},
		"success: implementation": {kind: KindImplementation, want: "implementation"},
		"success: incomingCall":   {kind: KindIncomingCall, want: "incomingCall"},
		"success: outgoingCall":   {kind: KindOutgoingCall, want: "outgoingCall"},
		"success: superType":      {kind: KindSuperType, want: "superType"},
		"success: subType":        {kind: KindSubType, want: "subType"},
		"success: zero value":     {kind: 0, want: "unknown"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := tt.kind.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRefKindIsHierarchy(t *testing.T) {
	t.Parallel()

	hierarchy := []RefKind{KindIncomingCall, KindOutgoingCall, KindSuperType, KindSubType}
	location := []RefKind{KindReference, KindDefinition, KindDeclaration, KindTypeDefinition, KindImplementation}

	for _, k := range hierarchy {
		if !k.isHierarchy() {
			t.Errorf("%s.isHierarchy() = false, want true", k)
		}
	}
	for _, k := range location {
		if k.isHierarchy() {
			t.Errorf("%s.isHierarchy() = true, want false", k)
		}
	}
}

func defaultBudgetPtr() *Budget {
	b := DefaultBudget()
	return &b
}

// callerRef builds an incoming-call hierarchy node at the given selection line.
func callerRef(uri string, line uint32) Ref {
	return Ref{URI: uri, SelectionStart: protocol.Position{Line: line}, Kind: KindIncomingCall}
}

// locationRef builds a location occurrence of kind at startLine in a.go.
func locationRef(startLine int, kind RefKind) Ref {
	return Ref{
		URI:   "file:///a.go",
		Range: lsp.NavigationRange{StartLine: startLine, StartColumn: 0, EndLine: startLine, EndColumn: 4},
		Kind:  kind,
	}
}

func TestTraversalVisitDedupHierarchy(t *testing.T) {
	t.Parallel()

	tr := NewTraversal(defaultBudgetPtr())
	node := callerRef("file:///a.go", 10)

	first, err := tr.Visit(node)
	if err != nil || !first {
		t.Fatalf("first Visit = (%v, %v), want (true, nil)", first, err)
	}
	second, err := tr.Visit(node)
	if err != nil || second {
		t.Fatalf("repeat Visit = (%v, %v), want (false, nil)", second, err)
	}
	if tr.Nodes() != 1 {
		t.Errorf("Nodes() = %d, want 1 (repeat visit must not bump the counter)", tr.Nodes())
	}
}

func TestTraversalVisitSelfCycleTerminates(t *testing.T) {
	t.Parallel()

	tr := NewTraversal(defaultBudgetPtr())
	a := callerRef("file:///a.go", 1)

	// A -> A: the second edge back into A is a repeat and must stop.
	if first, _ := tr.Visit(a); !first {
		t.Fatal("first visit of A should be new")
	}
	if first, _ := tr.Visit(a); first {
		t.Fatal("A->A cycle: revisiting A must report firstTime=false")
	}
	if tr.Nodes() != 1 {
		t.Errorf("Nodes() = %d, want 1", tr.Nodes())
	}
}

func TestTraversalVisitMutualCycleTerminates(t *testing.T) {
	t.Parallel()

	tr := NewTraversal(defaultBudgetPtr())
	a := callerRef("file:///a.go", 1)
	b := callerRef("file:///b.go", 2)

	// A -> B -> A: each node new once, then the edge back to A stops.
	steps := []struct {
		ref  Ref
		want bool
	}{
		{a, true},
		{b, true},
		{a, false},
	}
	for i, step := range steps {
		got, err := tr.Visit(step.ref)
		if err != nil {
			t.Fatalf("step %d Visit error: %v", i, err)
		}
		if got != step.want {
			t.Fatalf("step %d firstTime = %v, want %v", i, got, step.want)
		}
	}
	if tr.Nodes() != 2 {
		t.Errorf("Nodes() = %d, want 2", tr.Nodes())
	}
}

func TestTraversalVisitSymbolIdentityCollapsesOccurrences(t *testing.T) {
	t.Parallel()

	tr := NewTraversal(defaultBudgetPtr())
	// Same symbol (uri+selection start) reached via two different occurrence
	// ranges must collapse to one node under symbolKey.
	n1 := Ref{URI: "file:///a.go", SelectionStart: protocol.Position{Line: 5, Character: 8}, Range: lsp.NavigationRange{StartLine: 5}, Kind: KindSuperType}
	n2 := Ref{URI: "file:///a.go", SelectionStart: protocol.Position{Line: 5, Character: 8}, Range: lsp.NavigationRange{StartLine: 40}, Kind: KindSuperType}

	if first, _ := tr.Visit(n1); !first {
		t.Fatal("first symbol visit should be new")
	}
	if first, _ := tr.Visit(n2); first {
		t.Fatal("same symbol at a different occurrence must dedup on symbolKey")
	}
	if tr.Nodes() != 1 {
		t.Errorf("Nodes() = %d, want 1", tr.Nodes())
	}
}

func TestTraversalVisitDedupLocation(t *testing.T) {
	t.Parallel()

	tr := NewTraversal(defaultBudgetPtr())
	loc := locationRef(12, KindReference)

	if first, _ := tr.Visit(loc); !first {
		t.Fatal("first location visit should be new")
	}
	if first, _ := tr.Visit(loc); first {
		t.Fatal("identical occurrence must dedup on occurrenceKey")
	}
	if tr.Refs() != 1 {
		t.Errorf("Refs() = %d, want 1", tr.Refs())
	}
}

func TestTraversalVisitDistinguishesKindAtSameLocation(t *testing.T) {
	t.Parallel()

	tr := NewTraversal(defaultBudgetPtr())
	def := locationRef(3, KindDefinition)
	decl := locationRef(3, KindDeclaration)

	if first, _ := tr.Visit(def); !first {
		t.Fatal("definition at line 3 should be new")
	}
	if first, _ := tr.Visit(decl); !first {
		t.Fatal("declaration at the same line is a different kind and must be new")
	}
	if tr.Refs() != 2 {
		t.Errorf("Refs() = %d, want 2", tr.Refs())
	}
}

func TestTraversalVisitNodeBudget(t *testing.T) {
	t.Parallel()

	budget := DefaultBudget()
	budget.MaxNodes = 2
	tr := NewTraversal(&budget)

	if _, err := tr.Visit(callerRef("file:///a.go", 1)); err != nil {
		t.Fatalf("node 1 unexpected error: %v", err)
	}
	if _, err := tr.Visit(callerRef("file:///a.go", 2)); err != nil {
		t.Fatalf("node 2 unexpected error: %v", err)
	}
	first, err := tr.Visit(callerRef("file:///a.go", 3))
	if err == nil {
		t.Fatal("node 3 should exceed MaxNodes=2")
	}
	if !first {
		t.Error("the budget-exceeding visit is still a first sighting; firstTime should be true")
	}
}

func TestTraversalVisitReferenceBudget(t *testing.T) {
	t.Parallel()

	budget := DefaultBudget()
	budget.MaxReferences = 2
	tr := NewTraversal(&budget)

	if _, err := tr.Visit(locationRef(1, KindReference)); err != nil {
		t.Fatalf("ref 1 unexpected error: %v", err)
	}
	if _, err := tr.Visit(locationRef(2, KindReference)); err != nil {
		t.Fatalf("ref 2 unexpected error: %v", err)
	}
	if _, err := tr.Visit(locationRef(3, KindReference)); err == nil {
		t.Fatal("ref 3 should exceed MaxReferences=2")
	}
}

func TestTraversalAddFileBudget(t *testing.T) {
	t.Parallel()

	budget := DefaultBudget()
	budget.MaxFiles = 1
	tr := NewTraversal(&budget)

	if err := tr.AddFile(); err != nil {
		t.Fatalf("file 1 unexpected error: %v", err)
	}
	if err := tr.AddFile(); err == nil {
		t.Fatal("file 2 should exceed MaxFiles=1")
	}
}
