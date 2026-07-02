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
	"fmt"
	"testing"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

// fakeDiagLooker records the order of files it was asked about and returns a
// per-path canned result or error.
type fakeDiagLooker struct {
	byPath map[string][]lsp.Diagnostic
	errs   map[string]error
	order  []string
}

func (f *fakeDiagLooker) Lookup(_ context.Context, _, absPath, _ string) ([]lsp.Diagnostic, error) {
	f.order = append(f.order, absPath)
	if err, ok := f.errs[absPath]; ok {
		return nil, err
	}
	return f.byPath[absPath], nil
}

func diagFile(path string) DiagFile { return DiagFile{Path: path, Text: "package main\n"} }

func TestDiagnosticsFacadeCollectEpicenterFirst(t *testing.T) {
	t.Parallel()

	looker := &fakeDiagLooker{
		byPath: map[string][]lsp.Diagnostic{
			"/ws/epicenter.go": {{StartLine: 1, Severity: "error", Message: "boom"}},
			"/ws/a.go":         {{StartLine: 2, Severity: "warning", Message: "meh"}},
		},
	}
	facade := NewDiagnosticsFacade(looker)
	budget := DefaultBudget()

	got := facade.Collect(t.Context(), "go", diagFile("/ws/epicenter.go"), []DiagFile{diagFile("/ws/a.go")}, &budget)

	if len(got.Files) != 2 {
		t.Fatalf("Files = %d, want 2", len(got.Files))
	}
	if got.Files[0].Path != "/ws/epicenter.go" {
		t.Errorf("first file = %q, want the epicenter", got.Files[0].Path)
	}
	if looker.order[0] != "/ws/epicenter.go" {
		t.Errorf("first lookup = %q, want the epicenter to be queried first", looker.order[0])
	}
	if got.Files[0].Leg.Status != StatusOK {
		t.Errorf("epicenter leg status = %v, want ok", got.Files[0].Leg.Status)
	}
	if got.Omitted != 0 {
		t.Errorf("Omitted = %d, want 0", got.Omitted)
	}
}

func TestDiagnosticsFacadeCollectCapsFileCount(t *testing.T) {
	t.Parallel()

	looker := &fakeDiagLooker{byPath: map[string][]lsp.Diagnostic{}}
	facade := NewDiagnosticsFacade(looker)
	budget := DefaultBudget()
	budget.MaxDiagFiles = 3 // epicenter + at most 2 others

	others := []DiagFile{
		diagFile("/ws/a.go"),
		diagFile("/ws/b.go"),
		diagFile("/ws/c.go"),
		diagFile("/ws/d.go"),
	}
	got := facade.Collect(t.Context(), "go", diagFile("/ws/epicenter.go"), others, &budget)

	if len(got.Files) != 3 {
		t.Fatalf("Files = %d, want 3 (epicenter + 2 others)", len(got.Files))
	}
	if got.Omitted != 2 {
		t.Errorf("Omitted = %d, want 2", got.Omitted)
	}
	if len(looker.order) != 3 {
		t.Errorf("lookups issued = %d, want 3 (dropped files must not be queried)", len(looker.order))
	}
}

func TestDiagnosticsFacadeCollectMapsLegStatuses(t *testing.T) {
	t.Parallel()

	unsupported := fmt.Errorf("diagnostics: %w", lsp.ErrUnsupported)
	failure := errors.New("server crashed")
	looker := &fakeDiagLooker{
		byPath: map[string][]lsp.Diagnostic{
			"/ws/epicenter.go": {{Severity: "error", Message: "boom"}},
			"/ws/empty.go":     {},
		},
		errs: map[string]error{
			"/ws/unsupported.go": unsupported,
			"/ws/broken.go":      failure,
		},
	}
	facade := NewDiagnosticsFacade(looker)
	budget := DefaultBudget()

	others := []DiagFile{
		diagFile("/ws/empty.go"),
		diagFile("/ws/unsupported.go"),
		diagFile("/ws/broken.go"),
	}
	got := facade.Collect(t.Context(), "go", diagFile("/ws/epicenter.go"), others, &budget)

	want := map[string]LegStatus{
		"/ws/epicenter.go":   StatusOK,
		"/ws/empty.go":       StatusEmpty,
		"/ws/unsupported.go": StatusUnsupported,
		"/ws/broken.go":      StatusError,
	}
	for _, fd := range got.Files {
		if fd.Leg.Status != want[fd.Path] {
			t.Errorf("%s leg status = %v, want %v", fd.Path, fd.Leg.Status, want[fd.Path])
		}
	}
}

func TestDiagnosticsFacadeCollectZeroBudgetKeepsOnlyEpicenter(t *testing.T) {
	t.Parallel()

	looker := &fakeDiagLooker{byPath: map[string][]lsp.Diagnostic{}}
	facade := NewDiagnosticsFacade(looker)
	budget := DefaultBudget()
	budget.MaxDiagFiles = 0

	got := facade.Collect(t.Context(), "go", diagFile("/ws/epicenter.go"), []DiagFile{diagFile("/ws/a.go")}, &budget)

	if len(got.Files) != 1 {
		t.Fatalf("Files = %d, want 1 (epicenter is always gathered)", len(got.Files))
	}
	if got.Files[0].Path != "/ws/epicenter.go" {
		t.Errorf("kept file = %q, want the epicenter", got.Files[0].Path)
	}
	if got.Omitted != 1 {
		t.Errorf("Omitted = %d, want 1", got.Omitted)
	}
}
