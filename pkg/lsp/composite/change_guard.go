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
	"context"
	"errors"
	"slices"
	"strings"

	"go.lsp.dev/protocol"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

// verdictHedge is the fixed caveat attached to every change_guard result: the
// verdict reflects only static, settled diagnostics, so the agent — not the
// tool — owns the decision to ship.
const verdictHedge = "advisory only: reflects static diagnostics settled at analysis time, " +
	"not tests or runtime; cold or unsettled diagnostics yield notReady, never clean"

// ChangeGuard reads the post-edit, on-disk state of a changed file and reports
// an advisory verdict on whether the change looks safe. It does not simulate:
// the agent has already written the file. Diagnostics is the epicenter leg and
// is readiness-gated per transport mode, so a cold or unsettled diagnostics
// result never renders a clean verdict.
type ChangeGuard struct {
	engine *Engine
	budget Budget
}

// NewChangeGuard returns a verify-after-edit composite backed by engine.
func NewChangeGuard(engine *Engine) *ChangeGuard {
	return &ChangeGuard{engine: engine, budget: DefaultBudget()}
}

// Verdict is the advisory judgement change_guard emits.
type Verdict string

const (
	// VerdictClean means diagnostics settled with nothing to report.
	VerdictClean Verdict = "clean"
	// VerdictAttention means diagnostics reported warnings or hints but no errors.
	VerdictAttention Verdict = "attention"
	// VerdictBroken means diagnostics reported at least one error-severity item.
	VerdictBroken Verdict = "broken"
)

// ChangeGuardResult is the assembled verify-after-edit report with zero-based
// positions. AdvisoryVerdict is empty when the diagnostics epicenter was not
// ready (see Meta.Readiness). Basis names the legs that produced the verdict,
// and Hedge is the fixed advisory caveat.
type ChangeGuardResult struct {
	AdvisoryVerdict    Verdict
	Basis              []string
	Hedge              string
	Diagnostics        Leg[[]lsp.Diagnostic]
	ChangedSymbols     []string
	DisappearedSymbols []string
	QuickFixes         Leg[[]lsp.CodeAction]
	Meta               Meta
}

var changeGuardCapabilities = []Capability{CapDiagnostics, CapDocumentSymbol, CapCodeAction}

// Analyze reads absPath's current diagnostics and reports an advisory verdict.
// text is the post-edit on-disk content, read once and threaded through every
// leg. baseline, when non-nil, is the caller's pre-edit document symbol outline
// used to compute which symbols changed or disappeared. If the diagnostics
// capability is unsupported Analyze returns an error wrapping
// [lsp.ErrUnsupported]; if diagnostics are not ready it returns a result with
// no verdict and readiness "notReady".
func (cg *ChangeGuard) Analyze(ctx context.Context, lang, absPath, text string, baseline []lsp.DocumentSymbolEntry) (ChangeGuardResult, error) {
	report, err := Report(ctx, cg.engine.capabilities, lang, changeGuardCapabilities)
	if err != nil {
		return ChangeGuardResult{}, err
	}

	diags, readiness, err := cg.epicenterDiagnostics(ctx, lang, absPath, text)
	if err != nil {
		return ChangeGuardResult{}, err
	}

	meta := Meta{
		Readiness:           readinessString(readiness),
		EpicenterTextHash:   hashText(text),
		CapabilitiesUsed:    report.Used,
		CapabilitiesMissing: report.Missing,
	}

	// Never render a verdict from diagnostics that did not become ready: an
	// empty result from a cold server is not a clean bill of health.
	if readiness != lsp.ReadinessStable {
		meta.StopReason = StopExhausted.String()
		return ChangeGuardResult{
			Hedge:       verdictHedge,
			Diagnostics: Leg[[]lsp.Diagnostic]{Status: StatusNotReady, Note: "diagnostics did not become ready within the epicenter phase"},
			Meta:        meta,
		}, nil
	}

	meta.StopReason = StopStable.String()
	result := ChangeGuardResult{
		AdvisoryVerdict: verdictFor(diags),
		Basis:           []string{"diagnostics"},
		Hedge:           verdictHedge,
		Diagnostics:     LegFrom(diags, nil),
		Meta:            meta,
	}

	if baseline != nil {
		changed, disappeared, symErr := cg.symbolDiff(ctx, lang, absPath, text, baseline)
		if symErr == nil {
			result.ChangedSymbols = changed
			result.DisappearedSymbols = disappeared
			result.Basis = append(result.Basis, "documentSymbol")
		}
	}

	result.QuickFixes = cg.quickFixesLeg(ctx, lang, absPath, text, diags)
	if result.QuickFixes.Status == StatusOK {
		result.Basis = append(result.Basis, "codeAction")
	}

	return result, nil
}

// epicenterDiagnostics resolves the changed file's diagnostics with a
// per-transport readiness gate. On pull servers, where a single request has no
// settle machinery, it retries under Stable until two results agree. On push
// servers the settle machinery is inside the lookup: a not-settled file
// surfaces as a deadline error, which maps to a not-ready readiness rather than
// a hard error.
func (cg *ChangeGuard) epicenterDiagnostics(ctx context.Context, lang, absPath, text string) ([]lsp.Diagnostic, lsp.Readiness, error) {
	snap, err := cg.engine.capabilities.CapabilitySnapshot(ctx, lang)
	if err != nil {
		return nil, lsp.ReadinessExhausted, err
	}

	envCtx, cancel := context.WithTimeout(ctx, cg.budget.ReadinessEnvelope)
	defer cancel()

	if snap.PullDiagnostics {
		cfg := lsp.StableConfig{Attempts: cg.budget.ReadinessAttempts, Delay: cg.budget.ReadinessDelay}
		return lsp.Stable(envCtx, cfg, canonicalDiagnostics, func(c context.Context) ([]lsp.Diagnostic, error) {
			return cg.engine.diagEpicenter.Lookup(c, lang, absPath, text)
		})
	}

	diags, err := cg.engine.diagEpicenter.Lookup(envCtx, lang, absPath, text)
	switch {
	case err == nil:
		return diags, lsp.ReadinessStable, nil
	case errors.Is(err, lsp.ErrUnsupported):
		// An unsupported epicenter must reach the caller as an error.
		return nil, lsp.ReadinessExhausted, err
	case errors.Is(err, context.DeadlineExceeded):
		// The push stream never settled within the envelope: not ready, not a
		// hard failure.
		return nil, lsp.ReadinessExhausted, nil
	default:
		return nil, lsp.ReadinessExhausted, err
	}
}

// symbolDiff compares the current outline against baseline, returning the names
// present now that were not before (changed/new) and the names present before
// that are gone now (disappeared).
func (cg *ChangeGuard) symbolDiff(ctx context.Context, lang, absPath, text string, baseline []lsp.DocumentSymbolEntry) (changed, disappeared []string, err error) {
	current, err := cg.engine.documentSymbol.Lookup(ctx, lang, absPath, text)
	if err != nil {
		return nil, nil, err
	}
	before := symbolNameSet(baseline)
	after := symbolNameSet(current)
	for name := range after {
		if _, ok := before[name]; !ok {
			changed = append(changed, name)
		}
	}
	for name := range before {
		if _, ok := after[name]; !ok {
			disappeared = append(disappeared, name)
		}
	}
	slices.Sort(changed)
	slices.Sort(disappeared)
	return changed, disappeared, nil
}

// quickFixesLeg gathers quick-fix code actions for the whole file, scoped to
// its diagnostics.
func (cg *ChangeGuard) quickFixesLeg(ctx context.Context, lang, absPath, text string, diags []lsp.Diagnostic) Leg[[]lsp.CodeAction] {
	rng := diagnosticsRange(diags)
	actions, err := cg.engine.codeAction.Lookup(ctx, lang, absPath, text, rng, []protocol.CodeActionKind{protocol.CodeActionKindQuickFix}, false)
	return LegFrom(actions, err)
}

// verdictFor maps a settled diagnostics set to an advisory verdict: any
// error-severity item is broken, any other diagnostic is attention, and an
// empty settled set is clean.
func verdictFor(diags []lsp.Diagnostic) Verdict {
	if len(diags) == 0 {
		return VerdictClean
	}
	for i := range diags {
		if strings.EqualFold(diags[i].Severity, "error") {
			return VerdictBroken
		}
	}
	return VerdictAttention
}

func symbolNameSet(entries []lsp.DocumentSymbolEntry) map[string]struct{} {
	names := make(map[string]struct{})
	var walk func([]lsp.DocumentSymbolEntry)
	walk = func(es []lsp.DocumentSymbolEntry) {
		for i := range es {
			names[es[i].Name] = struct{}{}
			if len(es[i].Children) > 0 {
				walk(es[i].Children)
			}
		}
	}
	walk(entries)
	return names
}

// canonicalDiagnostics sorts diagnostics so the pull-path readiness comparison
// is order-insensitive.
func canonicalDiagnostics(in []lsp.Diagnostic) []lsp.Diagnostic {
	slices.SortFunc(in, func(a, b lsp.Diagnostic) int {
		if a.StartLine != b.StartLine {
			return a.StartLine - b.StartLine
		}
		if a.StartColumn != b.StartColumn {
			return a.StartColumn - b.StartColumn
		}
		return strings.Compare(a.Message, b.Message)
	})
	return in
}

// diagnosticsRange returns a range spanning the diagnostics, so a single code
// action request covers every reported problem. It falls back to the file start
// when there are no diagnostics.
func diagnosticsRange(diags []lsp.Diagnostic) protocol.Range {
	if len(diags) == 0 {
		return protocol.Range{}
	}
	minLine, minCol := diags[0].StartLine, diags[0].StartColumn
	maxLine, maxCol := diags[0].EndLine, diags[0].EndColumn
	for i := range diags {
		if diags[i].StartLine < minLine || (diags[i].StartLine == minLine && diags[i].StartColumn < minCol) {
			minLine, minCol = diags[i].StartLine, diags[i].StartColumn
		}
		if diags[i].EndLine > maxLine || (diags[i].EndLine == maxLine && diags[i].EndColumn > maxCol) {
			maxLine, maxCol = diags[i].EndLine, diags[i].EndColumn
		}
	}
	return protocol.Range{
		Start: protocol.Position{Line: clampUint32(minLine), Character: clampUint32(minCol)},
		End:   protocol.Position{Line: clampUint32(maxLine), Character: clampUint32(maxCol)},
	}
}

// clampUint32 converts a zero-based line or column back to the protocol's
// uint32 representation, clamping out-of-range values rather than wrapping.
// Diagnostic coordinates originate from the server as uint32, so the clamp is a
// safety net, not an expected path.
func clampUint32(v int) uint32 {
	if v < 0 {
		return 0
	}
	if v > int(^uint32(0)) {
		return ^uint32(0)
	}
	return uint32(v)
}
