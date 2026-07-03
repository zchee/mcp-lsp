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

package gointegration

import (
	"testing"
	"time"

	"github.com/zchee/mcp-lsp/pkg/lsp/composite"
	"github.com/zchee/mcp-lsp/tests/internal/lsptest"
)

// TestIntegrationImpactAnalysisWithGopls drives the impact-analysis composite
// against real gopls over the references fixture: the epicenter references and
// call graph populate, the declaration leg reports unsupported (gopls has no
// textDocument/declaration), and the type-graph leg populates.
func TestIntegrationImpactAnalysisWithGopls(t *testing.T) {
	requireIntegration(t)

	ws := extractFixture(t, "references_suite.txtar")
	mgr := newManager(t, ws)
	engine := composite.NewEngine(mgr)
	impact := composite.NewImpactAnalysis(engine)

	greetFile := ws.Path("greet.go")
	greetText := ws.Source(t, "greet.go")
	declPos := ws.MarkerPosition(t, "greet.go", "decl", "Greeting")

	var res composite.ImpactResult
	deadline := time.Now().Add(30 * time.Second)
	for {
		var err error
		res, err = impact.Analyze(t.Context(), "go", greetFile, greetText, declPos)
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if res.References.Status == composite.StatusOK && len(res.References.Data) >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("references never populated: status=%v count=%d readiness=%s", res.References.Status, len(res.References.Data), res.Meta.Readiness)
		}
		if ctxErr := lsptest.SleepOrCancel(t.Context(), 250*time.Millisecond); ctxErr != nil {
			t.Fatalf("context canceled: %v", ctxErr)
		}
	}

	if res.Meta.Readiness != "stable" {
		t.Errorf("Meta.Readiness = %q, want stable", res.Meta.Readiness)
	}
	if res.Declaration.Status != composite.StatusUnsupported {
		t.Errorf("Declaration leg = %v, want unsupported (gopls lacks textDocument/declaration)", res.Declaration.Status)
	}
	if res.CallGraph.Status == composite.StatusError {
		t.Errorf("CallGraph leg errored: %s", res.CallGraph.Note)
	}
	if len(res.AffectedFiles) == 0 {
		t.Error("AffectedFiles is empty, want at least the epicenter and caller files")
	}
}

// TestIntegrationSymbolContextWithGopls drives the symbol-context composite
// against real gopls: the hover epicenter and definition leg populate.
func TestIntegrationSymbolContextWithGopls(t *testing.T) {
	requireIntegration(t)

	ws := extractFixture(t, "references_suite.txtar")
	mgr := newManager(t, ws)
	symbolCtx := composite.NewSymbolContext(composite.NewEngine(mgr))

	mainFile := ws.Path("main.go")
	mainText := ws.Source(t, "main.go")
	usePos := ws.MarkerPosition(t, "main.go", "use", "Greeting")

	var res composite.SymbolContextResult
	deadline := time.Now().Add(30 * time.Second)
	for {
		var err error
		res, err = symbolCtx.Analyze(t.Context(), "go", mainFile, mainText, usePos)
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if res.Hover != nil && res.Definitions.Status == composite.StatusOK {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("symbol context never populated: hover=%v definitions=%v", res.Hover != nil, res.Definitions.Status)
		}
		if ctxErr := lsptest.SleepOrCancel(t.Context(), 250*time.Millisecond); ctxErr != nil {
			t.Fatalf("context canceled: %v", ctxErr)
		}
	}

	if res.Hover == nil || res.Hover.Value == "" {
		t.Errorf("Hover = %+v, want a populated hover card", res.Hover)
	}
	if res.Meta.EpicenterTextHash == "" {
		t.Error("Meta.EpicenterTextHash is empty")
	}
}

// TestIntegrationChangeGuardBrokenWithGopls drives change_guard against a file
// with a deliberate compile error and asserts a broken verdict whose basis
// names diagnostics.
func TestIntegrationChangeGuardBrokenWithGopls(t *testing.T) {
	requireIntegration(t)

	ws := extractFixture(t, "change_guard_suite.txtar")
	mgr := newManager(t, ws)
	guard := composite.NewChangeGuard(composite.NewEngine(mgr))

	brokenFile := ws.Path("broken.go")
	brokenText := ws.Source(t, "broken.go")

	var res composite.ChangeGuardResult
	deadline := time.Now().Add(30 * time.Second)
	for {
		var err error
		res, err = guard.Analyze(t.Context(), "go", brokenFile, brokenText, nil)
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if res.Meta.Readiness == "stable" && res.AdvisoryVerdict == composite.VerdictBroken {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("broken verdict never produced: readiness=%s verdict=%q", res.Meta.Readiness, res.AdvisoryVerdict)
		}
		if ctxErr := lsptest.SleepOrCancel(t.Context(), 250*time.Millisecond); ctxErr != nil {
			t.Fatalf("context canceled: %v", ctxErr)
		}
	}

	if res.AdvisoryVerdict != composite.VerdictBroken {
		t.Errorf("AdvisoryVerdict = %q, want broken", res.AdvisoryVerdict)
	}
	if !containsString(res.Basis, "diagnostics") {
		t.Errorf("Basis = %v, want it to name diagnostics", res.Basis)
	}
	if res.Hedge == "" {
		t.Error("Hedge is empty, want the advisory caveat")
	}
}

func containsString(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}
