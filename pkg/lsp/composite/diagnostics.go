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

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

// diagnosticsLooker returns a file's current diagnostics. It is satisfied by
// *lsp.Diagnostics; the facade depends on the interface so tests can substitute
// a fake.
type diagnosticsLooker interface {
	Lookup(ctx context.Context, lang, absPath, text string) ([]lsp.Diagnostic, error)
}

// Compile-time proof that the concrete diagnostics helper satisfies the facade.
var _ diagnosticsLooker = (*lsp.Diagnostics)(nil)

// DiagFile is one file to diagnose: its path and the exact text to sync before
// the lookup.
type DiagFile struct {
	Path string
	Text string
}

// FileDiagnostics pairs a file path with the diagnostics leg for it.
type FileDiagnostics struct {
	Path string
	Leg  Leg[[]lsp.Diagnostic]
}

// DiagnosticsResult is the outcome of a bounded diagnostics gather: one leg per
// file that was queried (epicenter first), plus how many extra files were
// dropped because they exceeded the file cap.
type DiagnosticsResult struct {
	Files   []FileDiagnostics
	Omitted int
}

// DiagnosticsFacade gathers per-file diagnostics through a [diagnosticsLooker],
// bounding the number of files it opens so a fan-out over a large blast radius
// cannot pay an unbounded settle cost.
type DiagnosticsFacade struct {
	looker diagnosticsLooker
}

// NewDiagnosticsFacade wraps looker.
func NewDiagnosticsFacade(looker diagnosticsLooker) *DiagnosticsFacade {
	return &DiagnosticsFacade{looker: looker}
}

// Collect gathers diagnostics for the epicenter file first, then for as many of
// others as the file budget allows: at most Budget.MaxDiagFiles files total,
// counting the epicenter. Files beyond the cap are dropped and counted in
// Omitted rather than silently discarded. Each file's error is classified with
// the same rules as any other leg, so an unsupported capability is
// distinguishable from a genuine failure.
func (f *DiagnosticsFacade) Collect(ctx context.Context, lang string, epicenter DiagFile, others []DiagFile, budget *Budget) DiagnosticsResult {
	result := DiagnosticsResult{
		Files: []FileDiagnostics{f.lookupFile(ctx, lang, epicenter)},
	}

	remaining := max(budget.MaxDiagFiles-1, 0)
	if len(others) > remaining {
		result.Omitted = len(others) - remaining
		others = others[:remaining]
	}
	for _, file := range others {
		result.Files = append(result.Files, f.lookupFile(ctx, lang, file))
	}
	return result
}

func (f *DiagnosticsFacade) lookupFile(ctx context.Context, lang string, file DiagFile) FileDiagnostics {
	diags, err := f.looker.Lookup(ctx, lang, file.Path, file.Text)
	return FileDiagnostics{Path: file.Path, Leg: LegFrom(diags, err)}
}
