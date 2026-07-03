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
	"context"
	"errors"
	"slices"

	"go.lsp.dev/protocol"
	"golang.org/x/sync/errgroup"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

// ImpactAnalysis computes the blast radius of changing a symbol: who references
// it, who calls it, what implements it, and what is already broken around it.
// References is the epicenter leg — it is readiness-gated so a cold-indexing
// server never reports an authoritative zero, and if it does not stabilize the
// whole fan-out is skipped. Every other leg degrades independently.
type ImpactAnalysis struct {
	engine *Engine
	budget Budget
}

// NewImpactAnalysis returns a blast-radius composite backed by engine.
func NewImpactAnalysis(engine *Engine) *ImpactAnalysis {
	return &ImpactAnalysis{engine: engine, budget: DefaultBudget()}
}

// CallEdge is one caller/callee relationship in the bounded call graph.
type CallEdge struct {
	Caller Ref
	Callee Ref
}

// ImpactResult is the assembled blast radius with zero-based positions. Each
// leg is a [Leg] carrying its own status; AffectedFiles is the deduplicated set
// of files touched by references and the call graph; Meta reports readiness,
// the stop reason, and capability availability.
type ImpactResult struct {
	References      Leg[[]Ref]
	CallGraph       Leg[[]CallEdge]
	TypeGraph       Leg[[]Ref]
	Implementations Leg[[]lsp.NavigationLocation]
	Definitions     Leg[[]lsp.NavigationLocation]
	Declaration     Leg[[]lsp.NavigationLocation]
	TypeDefinition  Leg[[]lsp.NavigationLocation]
	Diagnostics     Leg[[]FileDiagnostics]
	AffectedFiles   []string
	Meta            Meta
}

var impactCapabilities = []Capability{
	CapReferences, CapDefinition, CapDeclaration, CapTypeDefinition,
	CapImplementation, CapCallHierarchy, CapTypeHierarchy, CapDiagnostics,
}

// Analyze computes the blast radius for the symbol at pos in absPath. text is
// read once from disk and threaded unchanged through every leg. References is
// the epicenter: if its capability is unsupported Analyze returns an error
// wrapping [lsp.ErrUnsupported]; if it fails to stabilize within the readiness
// envelope Analyze returns early with readiness "notReady" and no fan-out.
func (ia *ImpactAnalysis) Analyze(ctx context.Context, lang, absPath, text string, pos protocol.Position) (ImpactResult, error) {
	report, err := Report(ctx, ia.engine.capabilities, lang, impactCapabilities)
	if err != nil {
		return ImpactResult{}, err
	}

	refs, readiness, err := ia.epicenterReferences(ctx, lang, absPath, text, pos)
	if err != nil {
		return ImpactResult{}, err
	}

	meta := Meta{
		Readiness:           readinessString(readiness),
		EpicenterTextHash:   hashText(text),
		CapabilitiesUsed:    report.Used,
		CapabilitiesMissing: report.Missing,
	}

	// A not-ready epicenter short-circuits the fan-out: traversing unstable
	// references would only produce a misleading partial graph.
	if readiness != lsp.ReadinessStable {
		meta.StopReason = StopExhausted.String()
		return ImpactResult{
			References: Leg[[]Ref]{Status: StatusNotReady, Note: "references did not stabilize within the readiness envelope"},
			Meta:       meta,
		}, nil
	}

	result := ImpactResult{
		References: LegFrom(refs, nil),
		Meta:       meta,
	}

	fanCtx, cancel := context.WithTimeout(ctx, ia.budget.Deadline)
	defer cancel()
	deadlineHit := ia.fanOut(fanCtx, lang, absPath, text, pos, &result)

	result.AffectedFiles = ia.affectedFiles(refs, result.CallGraph.Data)
	result.Meta.StopReason = impactStopReason(deadlineHit, ia.budgetFired(&result)).String()
	return result, nil
}

// epicenterReferences resolves references under the readiness gate within the
// readiness envelope. It returns the canonicalized references, the readiness
// outcome, and an error only when the capability is unsupported or the lookup
// fails for a non-readiness reason.
func (ia *ImpactAnalysis) epicenterReferences(ctx context.Context, lang, absPath, text string, pos protocol.Position) ([]Ref, lsp.Readiness, error) {
	envCtx, cancel := context.WithTimeout(ctx, ia.budget.ReadinessEnvelope)
	defer cancel()

	cfg := lsp.StableConfig{Attempts: ia.budget.ReadinessAttempts, Delay: ia.budget.ReadinessDelay}
	locs, readiness, err := lsp.Stable(envCtx, cfg, canonicalLocations, func(c context.Context) ([]lsp.NavigationLocation, error) {
		return ia.engine.references.Lookup(c, lang, absPath, text, pos, true)
	})
	if err != nil {
		return nil, readiness, err
	}
	return locationsToRefs(locs, KindReference), readiness, nil
}

// fanOut runs every secondary leg concurrently, bounded by MaxInflight, each
// goroutine writing exactly one result field. It reports whether the fan-out
// deadline fired.
func (ia *ImpactAnalysis) fanOut(ctx context.Context, lang, absPath, text string, pos protocol.Position, result *ImpactResult) bool {
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(ia.budget.MaxInflight)

	g.Go(func() error {
		locs, err := ia.engine.definition.Lookup(gctx, lang, absPath, text, pos)
		result.Definitions = legFromFanOut(locs, err)
		return nil
	})
	g.Go(func() error {
		locs, err := ia.engine.declaration.Lookup(gctx, lang, absPath, text, pos)
		result.Declaration = legFromFanOut(locs, err)
		return nil
	})
	g.Go(func() error {
		locs, err := ia.engine.typeDefinition.Lookup(gctx, lang, absPath, text, pos)
		result.TypeDefinition = legFromFanOut(locs, err)
		return nil
	})
	g.Go(func() error {
		locs, err := ia.engine.implementation.Lookup(gctx, lang, absPath, text, pos)
		result.Implementations = legFromFanOut(locs, err)
		return nil
	})
	g.Go(func() error {
		result.CallGraph = ia.callGraphLeg(gctx, lang, absPath, text, pos)
		return nil
	})
	g.Go(func() error {
		result.TypeGraph = ia.typeGraphLeg(gctx, lang, absPath, text, pos)
		return nil
	})
	g.Go(func() error {
		result.Diagnostics = ia.diagnosticsLeg(gctx, lang, absPath, text)
		return nil
	})

	_ = g.Wait()
	return errors.Is(ctx.Err(), context.DeadlineExceeded)
}

// callGraphLeg walks the call hierarchy from the symbol, bounded by the
// traversal budget and cycle-guarded. Depth 0 is the prepared root; each level
// expands the incoming and outgoing calls of the previous level.
func (ia *ImpactAnalysis) callGraphLeg(ctx context.Context, lang, absPath, text string, pos protocol.Position) Leg[[]CallEdge] {
	roots, err := ia.engine.callHierarchy.Prepare(ctx, lang, absPath, text, pos)
	if err != nil {
		return legFromEdges(nil, err)
	}

	tr := NewTraversal(&ia.budget)
	var edges []CallEdge
	frontier := roots
	for depth := 0; depth < ia.budget.MaxDepth && len(frontier) > 0; depth++ {
		var next []protocol.CallHierarchyItem
		for i := range frontier {
			item := frontier[i]
			node := callItemRef(&item, KindIncomingCall)
			if first, verr := tr.Visit(node); verr != nil {
				return Leg[[]CallEdge]{Status: StatusTruncated, Data: edges, Note: verr.Error()}
			} else if !first {
				continue
			}
			incoming, ierr := ia.engine.callHierarchy.IncomingCalls(ctx, lang, &item)
			if ierr == nil {
				for j := range incoming {
					caller := callItemRef(&incoming[j].From, KindIncomingCall)
					edges = append(edges, CallEdge{Caller: caller, Callee: node})
					next = append(next, incoming[j].From)
				}
			}
			outgoing, oerr := ia.engine.callHierarchy.OutgoingCalls(ctx, lang, &item)
			if oerr == nil {
				for j := range outgoing {
					callee := callItemRef(&outgoing[j].To, KindOutgoingCall)
					edges = append(edges, CallEdge{Caller: node, Callee: callee})
					next = append(next, outgoing[j].To)
				}
			}
		}
		frontier = next
	}
	return legFromEdges(edges, nil)
}

// typeGraphLeg collects the immediate supertypes and subtypes of the symbol.
// Type hierarchy is advertised only by some servers (gopls, not rust-analyzer
// or basedpyright); on the others the prepare call returns an error wrapping
// [lsp.ErrUnsupported], which surfaces as an unsupported leg.
func (ia *ImpactAnalysis) typeGraphLeg(ctx context.Context, lang, absPath, text string, pos protocol.Position) Leg[[]Ref] {
	roots, err := ia.engine.typeHierarchy.Prepare(ctx, lang, absPath, text, pos)
	if err != nil {
		return legFromRefs(nil, err)
	}

	var refs []Ref
	for i := range roots {
		supers, serr := ia.engine.typeHierarchy.Supertypes(ctx, lang, &roots[i])
		if serr == nil {
			refs = append(refs, typeItemsToRefs(supers, KindSuperType)...)
		}
		subs, berr := ia.engine.typeHierarchy.Subtypes(ctx, lang, &roots[i])
		if berr == nil {
			refs = append(refs, typeItemsToRefs(subs, KindSubType)...)
		}
	}
	return legFromRefs(refs, nil)
}

func typeItemsToRefs(items []protocol.TypeHierarchyItem, kind RefKind) []Ref {
	refs := make([]Ref, 0, len(items))
	for i := range items {
		refs = append(refs, Ref{
			URI:            string(items[i].URI),
			Range:          navigationRangeFromProtocolRange(items[i].Range),
			SelectionStart: items[i].SelectionRange.Start,
			Kind:           kind,
		})
	}
	return refs
}

func legFromRefs(refs []Ref, err error) Leg[[]Ref] {
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return Leg[[]Ref]{Status: StatusNotReady, Note: "fan-out deadline"}
		}
		if errors.Is(err, lsp.ErrUnsupported) {
			return Leg[[]Ref]{Status: StatusUnsupported, Note: err.Error()}
		}
		return Leg[[]Ref]{Status: StatusError, Note: err.Error()}
	}
	if len(refs) == 0 {
		return Leg[[]Ref]{Status: StatusEmpty}
	}
	return Leg[[]Ref]{Status: StatusOK, Data: refs}
}

// diagnosticsLeg gathers diagnostics for the epicenter file, capped by the
// diagnostics-file budget.
func (ia *ImpactAnalysis) diagnosticsLeg(ctx context.Context, lang, absPath, text string) Leg[[]FileDiagnostics] {
	if !ia.engine.hasDiagnostics() {
		return Leg[[]FileDiagnostics]{Status: StatusUnsupported, Note: "diagnostics facade not wired"}
	}
	res := ia.engine.diagnostics.Collect(ctx, lang, DiagFile{Path: absPath, Text: text}, nil, &ia.budget)
	if len(res.Files) == 0 {
		return Leg[[]FileDiagnostics]{Status: StatusEmpty}
	}
	status := StatusOK
	if res.Omitted > 0 {
		status = StatusTruncated
	}
	return Leg[[]FileDiagnostics]{Status: status, Data: res.Files}
}

// affectedFiles is the sorted, deduplicated set of file URIs touched by the
// references and the call graph.
func (ia *ImpactAnalysis) affectedFiles(refs []Ref, edges []CallEdge) []string {
	seen := make(map[string]struct{})
	add := func(uri string) {
		if uri != "" {
			seen[uri] = struct{}{}
		}
	}
	for i := range refs {
		add(refs[i].URI)
	}
	for i := range edges {
		add(edges[i].Caller.URI)
		add(edges[i].Callee.URI)
	}
	files := make([]string, 0, len(seen))
	for uri := range seen {
		files = append(files, uri)
	}
	slices.Sort(files)
	return files
}

func (ia *ImpactAnalysis) budgetFired(result *ImpactResult) bool {
	return result.CallGraph.Status == StatusTruncated || result.Diagnostics.Status == StatusTruncated
}

// impactStopReason applies the precedence exhausted > deadline > budget >
// stable. Exhausted is handled by the epicenter short-circuit before fan-out,
// so here the choice is between deadline, budget, and stable.
func impactStopReason(deadlineHit, budgetFired bool) StopReason {
	switch {
	case deadlineHit:
		return StopDeadline
	case budgetFired:
		return StopBudget
	default:
		return StopStable
	}
}

func legFromEdges(edges []CallEdge, err error) Leg[[]CallEdge] {
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return Leg[[]CallEdge]{Status: StatusNotReady, Note: "fan-out deadline"}
		}
		if errors.Is(err, lsp.ErrUnsupported) {
			return Leg[[]CallEdge]{Status: StatusUnsupported, Note: err.Error()}
		}
		return Leg[[]CallEdge]{Status: StatusError, Note: err.Error()}
	}
	if len(edges) == 0 {
		return Leg[[]CallEdge]{Status: StatusEmpty}
	}
	return Leg[[]CallEdge]{Status: StatusOK, Data: edges}
}

// canonicalLocations sorts navigation locations so the readiness comparison and
// output are deterministic across servers that reorder results.
func canonicalLocations(in []lsp.NavigationLocation) []lsp.NavigationLocation {
	slices.SortFunc(in, func(a, b lsp.NavigationLocation) int {
		if c := cmp.Compare(a.TargetURI, b.TargetURI); c != 0 {
			return c
		}
		if c := cmp.Compare(a.TargetRange.StartLine, b.TargetRange.StartLine); c != 0 {
			return c
		}
		return cmp.Compare(a.TargetRange.StartColumn, b.TargetRange.StartColumn)
	})
	return in
}

func locationsToRefs(locs []lsp.NavigationLocation, kind RefKind) []Ref {
	refs := make([]Ref, 0, len(locs))
	for i := range locs {
		refs = append(refs, Ref{URI: locs[i].TargetURI, Range: locs[i].TargetRange, Kind: kind})
	}
	return refs
}

func callItemRef(item *protocol.CallHierarchyItem, kind RefKind) Ref {
	return Ref{
		URI:            string(item.URI),
		Range:          navigationRangeFromProtocolRange(item.Range),
		SelectionStart: item.SelectionRange.Start,
		Kind:           kind,
	}
}

// navigationRangeFromProtocolRange converts a protocol range to the composite's
// zero-based navigation range. It mirrors the unexported converter in pkg/lsp,
// which the composite package cannot reach.
func navigationRangeFromProtocolRange(rng protocol.Range) lsp.NavigationRange {
	return lsp.NavigationRange{
		StartLine:   int(rng.Start.Line),
		StartColumn: int(rng.Start.Character),
		EndLine:     int(rng.End.Line),
		EndColumn:   int(rng.End.Character),
	}
}
