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
	"testing"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

func changeGuardEngine(snap lsp.CapabilitySnapshot, diag *fakeDiagnosticsLooker, docSym *fakeDocumentSymbolLooker, code *fakeCodeActionLooker) *Engine {
	return &Engine{
		diagEpicenter:  diag,
		documentSymbol: docSym,
		codeAction:     code,
		capabilities:   fakeCapabilityProbe{snap: snap},
	}
}

func newChangeGuard(engine *Engine) *ChangeGuard {
	cg := NewChangeGuard(engine)
	cg.budget = fastBudget()
	return cg
}

func errorDiag(msg string) lsp.Diagnostic {
	return lsp.Diagnostic{Severity: "error", Message: msg}
}

func TestChangeGuardPushSettledEmptyIsClean(t *testing.T) {
	t.Parallel()

	snap := lsp.CapabilitySnapshot{CodeAction: true} // PullDiagnostics false -> push path
	diag := &fakeDiagnosticsLooker{diags: nil}       // settled with nothing
	engine := changeGuardEngine(snap, diag, &fakeDocumentSymbolLooker{}, &fakeCodeActionLooker{})

	got, err := newChangeGuard(engine).Analyze(t.Context(), "go", "/ws/main.go", "package main\n", nil)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.AdvisoryVerdict != VerdictClean {
		t.Errorf("AdvisoryVerdict = %q, want clean (settled and empty)", got.AdvisoryVerdict)
	}
	if got.Meta.Readiness != "stable" {
		t.Errorf("Meta.Readiness = %q, want stable", got.Meta.Readiness)
	}
	if diag.calls != 1 {
		t.Errorf("push path issued %d diagnostics calls, want 1 (no double-settle)", diag.calls)
	}
}

func TestChangeGuardPushNotSettledIsNotReadyNeverClean(t *testing.T) {
	t.Parallel()

	snap := lsp.CapabilitySnapshot{} // push path
	// A cold push server that never settles surfaces as a deadline error.
	diag := &fakeDiagnosticsLooker{err: context.DeadlineExceeded}
	engine := changeGuardEngine(snap, diag, &fakeDocumentSymbolLooker{}, &fakeCodeActionLooker{})

	got, err := newChangeGuard(engine).Analyze(t.Context(), "go", "/ws/main.go", "package main\n", nil)
	if err != nil {
		t.Fatalf("Analyze must not hard-error on an unsettled push server: %v", err)
	}
	if got.AdvisoryVerdict != "" {
		t.Errorf("AdvisoryVerdict = %q, want empty (no verdict from unready diagnostics)", got.AdvisoryVerdict)
	}
	if got.Meta.Readiness != "notReady" {
		t.Errorf("Meta.Readiness = %q, want notReady", got.Meta.Readiness)
	}
	if got.Diagnostics.Status != StatusNotReady {
		t.Errorf("Diagnostics leg = %v, want notReady", got.Diagnostics.Status)
	}
}

func TestChangeGuardPullUnstableIsNotReadyNeverClean(t *testing.T) {
	t.Parallel()

	snap := lsp.CapabilitySnapshot{PullDiagnostics: true} // pull path -> Stable
	// Never stabilizes: each pull differs, so an empty tail is never trusted.
	diag := &fakeDiagnosticsLooker{seq: [][]lsp.Diagnostic{
		{errorDiag("a")},
		{errorDiag("b")},
		{errorDiag("c")},
	}}
	engine := changeGuardEngine(snap, diag, &fakeDocumentSymbolLooker{}, &fakeCodeActionLooker{})

	got, err := newChangeGuard(engine).Analyze(t.Context(), "go", "/ws/main.go", "package main\n", nil)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.AdvisoryVerdict != "" {
		t.Errorf("AdvisoryVerdict = %q, want empty (pull diagnostics never stabilized)", got.AdvisoryVerdict)
	}
	if got.Meta.Readiness != "notReady" {
		t.Errorf("Meta.Readiness = %q, want notReady", got.Meta.Readiness)
	}
}

func TestChangeGuardBrokenNamesDiagnosticsBasis(t *testing.T) {
	t.Parallel()

	snap := lsp.CapabilitySnapshot{CodeAction: true}
	diag := &fakeDiagnosticsLooker{diags: []lsp.Diagnostic{errorDiag("undefined: foo")}}
	code := &fakeCodeActionLooker{actions: []lsp.CodeAction{{Title: "import foo", Kind: "quickfix"}}}
	engine := changeGuardEngine(snap, diag, &fakeDocumentSymbolLooker{}, code)

	got, err := newChangeGuard(engine).Analyze(t.Context(), "go", "/ws/main.go", "package main\n", nil)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.AdvisoryVerdict != VerdictBroken {
		t.Errorf("AdvisoryVerdict = %q, want broken (error-severity diagnostic)", got.AdvisoryVerdict)
	}
	if !containsStr(got.Basis, "diagnostics") {
		t.Errorf("Basis = %v, want it to name diagnostics", got.Basis)
	}
	if got.QuickFixes.Status != StatusOK {
		t.Errorf("QuickFixes leg = %v, want ok", got.QuickFixes.Status)
	}
	if got.Hedge == "" {
		t.Error("Hedge is empty, want the fixed advisory caveat")
	}
}

func TestChangeGuardAttentionOnNonErrorDiagnostics(t *testing.T) {
	t.Parallel()

	snap := lsp.CapabilitySnapshot{}
	diag := &fakeDiagnosticsLooker{diags: []lsp.Diagnostic{{Severity: "warning", Message: "unused"}}}
	engine := changeGuardEngine(snap, diag, &fakeDocumentSymbolLooker{}, &fakeCodeActionLooker{})

	got, err := newChangeGuard(engine).Analyze(t.Context(), "go", "/ws/main.go", "package main\n", nil)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.AdvisoryVerdict != VerdictAttention {
		t.Errorf("AdvisoryVerdict = %q, want attention (warnings but no errors)", got.AdvisoryVerdict)
	}
}

func TestChangeGuardDiagnosticsUnsupportedErrors(t *testing.T) {
	t.Parallel()

	snap := lsp.CapabilitySnapshot{}
	diag := &fakeDiagnosticsLooker{err: unsupported("diagnostics")}
	engine := changeGuardEngine(snap, diag, &fakeDocumentSymbolLooker{}, &fakeCodeActionLooker{})

	_, err := newChangeGuard(engine).Analyze(t.Context(), "go", "/ws/main.go", "package main\n", nil)
	if !errors.Is(err, lsp.ErrUnsupported) {
		t.Fatalf("Analyze error = %v, want errors.Is lsp.ErrUnsupported (Diagnostics is the epicenter)", err)
	}
}

func TestChangeGuardSymbolDiff(t *testing.T) {
	t.Parallel()

	snap := lsp.CapabilitySnapshot{DocumentSymbol: true}
	diag := &fakeDiagnosticsLooker{diags: nil}
	// Current outline has NewFn but not OldFn; baseline had OldFn but not NewFn.
	current := &fakeDocumentSymbolLooker{entries: []lsp.DocumentSymbolEntry{{Name: "NewFn"}, {Name: "Kept"}}}
	engine := changeGuardEngine(snap, diag, current, &fakeCodeActionLooker{})
	baseline := []lsp.DocumentSymbolEntry{{Name: "OldFn"}, {Name: "Kept"}}

	got, err := newChangeGuard(engine).Analyze(t.Context(), "go", "/ws/main.go", "package main\n", baseline)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(got.ChangedSymbols) != 1 || got.ChangedSymbols[0] != "NewFn" {
		t.Errorf("ChangedSymbols = %v, want [NewFn]", got.ChangedSymbols)
	}
	if len(got.DisappearedSymbols) != 1 || got.DisappearedSymbols[0] != "OldFn" {
		t.Errorf("DisappearedSymbols = %v, want [OldFn]", got.DisappearedSymbols)
	}
	if !containsStr(got.Basis, "documentSymbol") {
		t.Errorf("Basis = %v, want it to name documentSymbol", got.Basis)
	}
}

func containsStr(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}
