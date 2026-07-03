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

package mcp

import (
	"context"
	"testing"

	"go.lsp.dev/protocol"

	"github.com/zchee/mcp-lsp/pkg/lsp"
	"github.com/zchee/mcp-lsp/pkg/lsp/composite"
)

type fakeSymbolContextAnalyzer struct {
	result composite.SymbolContextResult
	gotPos protocol.Position
	calls  int
}

func (f *fakeSymbolContextAnalyzer) Analyze(_ context.Context, _, _, _ string, pos protocol.Position) (composite.SymbolContextResult, error) {
	f.calls++
	f.gotPos = pos
	return f.result, nil
}

func TestSymbolContextHandlerConvertsPositionsAndMeta(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	analyzer := &fakeSymbolContextAnalyzer{
		result: composite.SymbolContextResult{
			Hover: &lsp.HoverResult{Kind: "markdown", Value: "doc", Range: &lsp.NavigationRange{StartLine: 4, StartColumn: 2, EndLine: 4, EndColumn: 8}},
			Definitions: composite.Leg[[]lsp.NavigationLocation]{
				Status: composite.StatusOK,
				Data:   []lsp.NavigationLocation{{TargetRange: lsp.NavigationRange{StartLine: 9, StartColumn: 1, EndLine: 9, EndColumn: 5}}},
			},
			Meta: composite.Meta{Readiness: "stable", StopReason: "stable", EpicenterTextHash: "abc", CapabilitiesUsed: []composite.Capability{composite.CapHover}},
		},
	}
	handler := symbolContextHandler(analyzer, t.TempDir(), testResolver(t, "go", "python", "rust"))

	_, out, err := handler(t.Context(), nil, SymbolContextInput{File: path, Line: 5, Column: 3, Language: "go"})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if want := (protocol.Position{Line: 4, Character: 2}); analyzer.gotPos != want {
		t.Errorf("analyzer position = %+v, want %+v (one-based input converted exactly once)", analyzer.gotPos, want)
	}
	if out.Hover == nil || out.Hover.Range.StartLine != 5 || out.Hover.Range.StartColumn != 3 {
		t.Errorf("hover range = %+v, want one-based 5:3", out.Hover)
	}
	if len(out.Definitions.Locations) != 1 || out.Definitions.Locations[0].StartLine != 10 {
		t.Errorf("definition location = %+v, want one-based start line 10", out.Definitions.Locations)
	}
	if out.Meta.Readiness != "stable" || out.Meta.EpicenterTextHash != "abc" {
		t.Errorf("meta not passed through: %+v", out.Meta)
	}
	if len(out.Meta.CapabilitiesUsed) != 1 || out.Meta.CapabilitiesUsed[0] != "hover" {
		t.Errorf("CapabilitiesUsed = %v, want [hover]", out.Meta.CapabilitiesUsed)
	}
}

type fakeImpactAnalyzer struct {
	result composite.ImpactResult
	gotPos protocol.Position
}

func (f *fakeImpactAnalyzer) Analyze(_ context.Context, _, _, _ string, pos protocol.Position) (composite.ImpactResult, error) {
	f.gotPos = pos
	return f.result, nil
}

func TestImpactAnalysisHandlerConvertsPositionsAndMeta(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	analyzer := &fakeImpactAnalyzer{
		result: composite.ImpactResult{
			References: composite.Leg[[]composite.Ref]{
				Status: composite.StatusOK,
				Data:   []composite.Ref{{Range: lsp.NavigationRange{StartLine: 3, StartColumn: 0, EndLine: 3, EndColumn: 4}}},
			},
			AffectedFiles: []string{"file:///a.go"},
			Meta:          composite.Meta{Readiness: "stable", StopReason: "stable", EpicenterTextHash: "hash"},
		},
	}
	handler := impactAnalysisHandler(analyzer, t.TempDir(), testResolver(t, "go", "python", "rust"))

	_, out, err := handler(t.Context(), nil, ImpactAnalysisInput{File: path, Line: 12, Column: 7, Language: "go"})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if want := (protocol.Position{Line: 11, Character: 6}); analyzer.gotPos != want {
		t.Errorf("analyzer position = %+v, want %+v", analyzer.gotPos, want)
	}
	if len(out.References.Locations) != 1 || out.References.Locations[0].StartLine != 4 {
		t.Errorf("reference location = %+v, want one-based start line 4", out.References.Locations)
	}
	if out.Meta.EpicenterTextHash != "hash" {
		t.Errorf("Meta.EpicenterTextHash = %q, want hash", out.Meta.EpicenterTextHash)
	}
}

type fakeChangeGuardAnalyzer struct {
	result composite.ChangeGuardResult
}

func (f *fakeChangeGuardAnalyzer) Analyze(_ context.Context, _, _, _ string, _ []lsp.DocumentSymbolEntry) (composite.ChangeGuardResult, error) {
	return f.result, nil
}

func TestChangeGuardHandlerConvertsDiagnosticsAndVerdict(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	analyzer := &fakeChangeGuardAnalyzer{
		result: composite.ChangeGuardResult{
			AdvisoryVerdict: composite.VerdictBroken,
			Basis:           []string{"diagnostics"},
			Hedge:           "advisory only",
			Diagnostics: composite.Leg[[]lsp.Diagnostic]{
				Status: composite.StatusOK,
				Data:   []lsp.Diagnostic{{StartLine: 6, StartColumn: 1, EndLine: 6, EndColumn: 9, Severity: "error", Message: "boom"}},
			},
			QuickFixes: composite.Leg[[]lsp.CodeAction]{Status: composite.StatusOK, Data: []lsp.CodeAction{{Title: "fix it"}}},
			Meta:       composite.Meta{Readiness: "stable", StopReason: "stable", EpicenterTextHash: "h"},
		},
	}
	handler := changeGuardHandler(analyzer, t.TempDir(), testResolver(t, "go", "python", "rust"))

	_, out, err := handler(t.Context(), nil, ChangeGuardInput{File: path, Language: "go"})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if out.AdvisoryVerdict != "broken" {
		t.Errorf("AdvisoryVerdict = %q, want broken", out.AdvisoryVerdict)
	}
	if len(out.Diagnostics) != 1 || out.Diagnostics[0].Range.StartLine != 7 {
		t.Errorf("diagnostic range = %+v, want one-based start line 7", out.Diagnostics)
	}
	if len(out.QuickFixes) != 1 || out.QuickFixes[0] != "fix it" {
		t.Errorf("QuickFixes = %v, want [fix it]", out.QuickFixes)
	}
	if out.Hedge != "advisory only" {
		t.Errorf("Hedge = %q, want the advisory caveat", out.Hedge)
	}
}
