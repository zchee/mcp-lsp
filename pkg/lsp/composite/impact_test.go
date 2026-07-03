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
	"errors"
	"testing"
	"time"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

// impactFakes holds the fakes an impact-analysis engine is wired from, so a
// test can inspect call counts after Analyze.
type impactFakes struct {
	refs     *fakeReferencesLooker
	def      *fakeNavLooker
	decl     *fakeNavLooker
	typeDef  *fakeNavLooker
	impl     *fakeNavLooker
	callHier *fakeCallHierarchyLooker
	typeHier *fakeTypeHierarchyLooker
	diag     *fakeDiagnosticsLooker
	snapshot lsp.CapabilitySnapshot
}

func impactEngine(f *impactFakes) *Engine {
	return &Engine{
		references:     f.refs,
		definition:     f.def,
		declaration:    f.decl,
		typeDefinition: f.typeDef,
		implementation: f.impl,
		callHierarchy:  f.callHier,
		typeHierarchy:  f.typeHier,
		diagnostics:    NewDiagnosticsFacade(f.diag),
		capabilities:   fakeCapabilityProbe{snap: f.snapshot},
	}
}

// fastBudget shortens the readiness envelope so the not-ready path does not
// spend seconds retrying in a unit test.
func fastBudget() Budget {
	b := DefaultBudget()
	b.ReadinessAttempts = 3
	b.ReadinessDelay = time.Millisecond
	b.ReadinessEnvelope = time.Second
	b.Deadline = time.Second
	return b
}

func stableImpactFakes() *impactFakes {
	return &impactFakes{
		refs:     &fakeReferencesLooker{locs: []lsp.NavigationLocation{{TargetURI: "file:///a.go", TargetRange: lsp.NavigationRange{StartLine: 4}}}},
		def:      &fakeNavLooker{locs: []lsp.NavigationLocation{{TargetURI: "file:///def.go"}}},
		decl:     &fakeNavLooker{err: unsupported("declaration")},
		typeDef:  &fakeNavLooker{locs: []lsp.NavigationLocation{{TargetURI: "file:///type.go"}}},
		impl:     &fakeNavLooker{locs: []lsp.NavigationLocation{{TargetURI: "file:///impl.go"}}},
		callHier: &fakeCallHierarchyLooker{},
		typeHier: &fakeTypeHierarchyLooker{},
		diag:     &fakeDiagnosticsLooker{},
		snapshot: lsp.CapabilitySnapshot{References: true, CallHierarchy: true, TypeDefinition: true, Implementation: true},
	}
}

func newImpact(engine *Engine) *ImpactAnalysis {
	ia := NewImpactAnalysis(engine)
	ia.budget = fastBudget()
	return ia
}

func TestImpactAnalyzeReferencesUnsupportedErrors(t *testing.T) {
	t.Parallel()

	f := stableImpactFakes()
	f.refs = &fakeReferencesLooker{err: unsupported("references")}
	f.snapshot = lsp.CapabilitySnapshot{}

	_, err := newImpact(impactEngine(f)).Analyze(t.Context(), "go", "/ws/main.go", "package main\n", protocol.Position{})
	if !errors.Is(err, lsp.ErrUnsupported) {
		t.Fatalf("Analyze error = %v, want errors.Is lsp.ErrUnsupported (References is the epicenter)", err)
	}
	if f.def.calls != 0 {
		t.Errorf("definition leg called %d times after epicenter failure, want 0", f.def.calls)
	}
}

func TestImpactAnalyzeNotReadyShortCircuitsFanOut(t *testing.T) {
	t.Parallel()

	f := stableImpactFakes()
	// Never stabilizes: every call returns a different reference set.
	f.refs = &fakeReferencesLooker{seq: [][]lsp.NavigationLocation{
		{{TargetURI: "file:///a.go", TargetRange: lsp.NavigationRange{StartLine: 1}}},
		{{TargetURI: "file:///a.go", TargetRange: lsp.NavigationRange{StartLine: 2}}},
		{{TargetURI: "file:///a.go", TargetRange: lsp.NavigationRange{StartLine: 3}}},
	}}

	got, err := newImpact(impactEngine(f)).Analyze(t.Context(), "go", "/ws/main.go", "package main\n", protocol.Position{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.References.Status != StatusNotReady {
		t.Errorf("References leg = %v, want notReady", got.References.Status)
	}
	if got.Meta.Readiness != "notReady" {
		t.Errorf("Meta.Readiness = %q, want notReady", got.Meta.Readiness)
	}
	if got.Meta.StopReason != StopExhausted.String() {
		t.Errorf("Meta.StopReason = %q, want exhausted", got.Meta.StopReason)
	}
	if f.def.calls != 0 || f.callHier.calls != 0 {
		t.Errorf("fan-out ran after a not-ready epicenter: def=%d callHier=%d, want 0/0", f.def.calls, f.callHier.calls)
	}
}

func TestImpactAnalyzeStableFanOutAndDegradation(t *testing.T) {
	t.Parallel()

	f := stableImpactFakes()
	got, err := newImpact(impactEngine(f)).Analyze(t.Context(), "go", "/ws/main.go", "package main\n", protocol.Position{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.References.Status != StatusOK {
		t.Errorf("References leg = %v, want ok", got.References.Status)
	}
	if got.Declaration.Status != StatusUnsupported {
		t.Errorf("Declaration leg = %v, want unsupported (gopls lacks it)", got.Declaration.Status)
	}
	if got.Implementations.Status != StatusOK {
		t.Errorf("Implementations leg = %v, want ok", got.Implementations.Status)
	}
	if got.Meta.Readiness != "stable" {
		t.Errorf("Meta.Readiness = %q, want stable", got.Meta.Readiness)
	}
	if !contains(got.Meta.CapabilitiesMissing, CapTypeHierarchy) {
		t.Errorf("CapabilitiesMissing = %v, want typeHierarchy (not advertised)", got.Meta.CapabilitiesMissing)
	}
}

func TestImpactAnalyzeTypeHierarchyUnsupported(t *testing.T) {
	t.Parallel()

	f := stableImpactFakes()
	f.typeHier = &fakeTypeHierarchyLooker{prepErr: unsupported("type hierarchy prepare")}

	got, err := newImpact(impactEngine(f)).Analyze(t.Context(), "go", "/ws/main.go", "package main\n", protocol.Position{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.TypeGraph.Status != StatusUnsupported {
		t.Errorf("TypeGraph leg = %v, want unsupported", got.TypeGraph.Status)
	}
}

func TestImpactAnalyzeCallGraphCycleTerminates(t *testing.T) {
	t.Parallel()

	f := stableImpactFakes()
	root := protocol.CallHierarchyItem{
		Name: "A", URI: uri.URI("file:///a.go"),
		SelectionRange: protocol.Range{Start: protocol.Position{Line: 1}},
	}
	// A's incoming caller is A itself: a self-cycle the traversal must break.
	f.callHier = &fakeCallHierarchyLooker{
		prepare:  []protocol.CallHierarchyItem{root},
		incoming: []protocol.CallHierarchyIncomingCall{{From: root}},
	}

	got, err := newImpact(impactEngine(f)).Analyze(t.Context(), "go", "/ws/main.go", "package main\n", protocol.Position{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	// The call graph must be produced without hanging; a self-edge yields at
	// most one edge and terminates.
	if got.CallGraph.Status == StatusError {
		t.Errorf("CallGraph leg errored: %s", got.CallGraph.Note)
	}
}

func TestImpactAnalyzeFanOutDeadlineIsNotReady(t *testing.T) {
	t.Parallel()

	f := stableImpactFakes()
	// A secondary leg that blocks past the fan-out deadline must report
	// notReady with the fan-out note, never an error, and must not abort the
	// composite.
	engine := impactEngine(f)
	engine.definition = blockingNavLooker{}

	ia := newImpact(engine)
	ia.budget.Deadline = time.Millisecond

	got, err := ia.Analyze(t.Context(), "go", "/ws/main.go", "package main\n", protocol.Position{})
	if err != nil {
		t.Fatalf("Analyze must not error when a fan-out leg exceeds the deadline: %v", err)
	}
	if got.Definitions.Status != StatusNotReady {
		t.Errorf("Definitions leg = %v, want notReady (cut short by the fan-out deadline)", got.Definitions.Status)
	}
	if got.Definitions.Note != "fan-out deadline" {
		t.Errorf("Definitions note = %q, want %q", got.Definitions.Note, "fan-out deadline")
	}
	if got.References.Status != StatusOK {
		t.Errorf("References leg = %v, want ok (the epicenter is unaffected)", got.References.Status)
	}
}

func TestImpactAnalyzeAffectedFilesDeduped(t *testing.T) {
	t.Parallel()

	f := stableImpactFakes()
	f.refs = &fakeReferencesLooker{locs: []lsp.NavigationLocation{
		{TargetURI: "file:///b.go", TargetRange: lsp.NavigationRange{StartLine: 1}},
		{TargetURI: "file:///a.go", TargetRange: lsp.NavigationRange{StartLine: 2}},
		{TargetURI: "file:///a.go", TargetRange: lsp.NavigationRange{StartLine: 9}},
	}}

	got, err := newImpact(impactEngine(f)).Analyze(t.Context(), "go", "/ws/main.go", "package main\n", protocol.Position{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	want := []string{"file:///a.go", "file:///b.go"}
	if len(got.AffectedFiles) != len(want) || got.AffectedFiles[0] != want[0] || got.AffectedFiles[1] != want[1] {
		t.Errorf("AffectedFiles = %v, want sorted-deduped %v", got.AffectedFiles, want)
	}
}
