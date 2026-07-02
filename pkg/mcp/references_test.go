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
	"errors"
	"fmt"
	"strings"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
	"go.lsp.dev/protocol"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

// fakeRefLooker is a refLooker test double recording its arguments and
// returning a canned result or error. errOnce makes only the first call fail,
// exercising the readiness gate's retry-through-transient-error path.
type fakeRefLooker struct {
	refs       []lsp.NavigationLocation
	err        error
	errOnce    bool
	gotLang    string
	gotPath    string
	gotText    string
	gotPos     protocol.Position
	gotInclude bool
	calls      int
}

func (f *fakeRefLooker) Lookup(_ context.Context, lang, absPath, text string, pos protocol.Position, includeDeclaration bool) ([]lsp.NavigationLocation, error) {
	f.calls++
	f.gotLang = lang
	f.gotPath = absPath
	f.gotText = text
	f.gotPos = pos
	f.gotInclude = includeDeclaration
	if f.err != nil {
		err := f.err
		if f.errOnce {
			f.err = nil
		}
		return nil, err
	}
	return f.refs, nil
}

func TestFindReferencesHandlerEmptyFile(t *testing.T) {
	t.Parallel()

	looker := &fakeRefLooker{}
	handler := referencesHandler(looker, t.TempDir(), testResolver(t, "go", "python", "rust"))

	_, _, err := handler(t.Context(), nil, ReferencesInput{Line: 1, Column: 1})
	if err == nil || !strings.Contains(err.Error(), "file is required") {
		t.Fatalf("handler error = %v, want file required error", err)
	}
	if looker.calls != 0 {
		t.Errorf("handler called Lookup %d times for invalid input, want 0", looker.calls)
	}
}

func TestFindReferencesHandlerInvalidLine(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	looker := &fakeRefLooker{}
	handler := referencesHandler(looker, t.TempDir(), testResolver(t, "go", "python", "rust"))

	_, _, err := handler(t.Context(), nil, ReferencesInput{File: path, Line: 0, Column: 1})
	if err == nil || !strings.Contains(err.Error(), "line must be greater than zero") {
		t.Fatalf("handler error = %v, want invalid line error", err)
	}
	if looker.calls != 0 {
		t.Errorf("handler called Lookup %d times for invalid input, want 0", looker.calls)
	}
}

func TestFindReferencesHandlerInvalidColumn(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	looker := &fakeRefLooker{}
	handler := referencesHandler(looker, t.TempDir(), testResolver(t, "go", "python", "rust"))

	_, _, err := handler(t.Context(), nil, ReferencesInput{File: path, Line: 1, Column: 0})
	if err == nil || !strings.Contains(err.Error(), "column must be greater than zero") {
		t.Fatalf("handler error = %v, want invalid column error", err)
	}
	if looker.calls != 0 {
		t.Errorf("handler called Lookup %d times for invalid input, want 0", looker.calls)
	}
}

func TestFindReferencesHandlerMissingFile(t *testing.T) {
	t.Parallel()

	looker := &fakeRefLooker{}
	handler := referencesHandler(looker, t.TempDir(), testResolver(t, "go", "python", "rust"))

	_, _, err := handler(t.Context(), nil, ReferencesInput{File: "/does/not/exist.go", Line: 1, Column: 1})
	if err == nil || !strings.Contains(err.Error(), "read file") {
		t.Fatalf("handler error = %v, want read file error", err)
	}
	if looker.calls != 0 {
		t.Errorf("handler called Lookup %d times for a missing file, want 0", looker.calls)
	}
}

func TestFindReferencesHandlerConvertsInputToZeroBasedAndForwardsInclude(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	looker := &fakeRefLooker{}
	handler := referencesHandler(looker, t.TempDir(), testResolver(t, "go", "python", "rust"))

	_, out, err := handler(t.Context(), nil, ReferencesInput{File: path, Line: 10, Column: 5, Language: "go", IncludeDeclaration: true})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if want := (protocol.Position{Line: 9, Character: 4}); looker.gotPos != want {
		t.Errorf("Lookup position = %+v, want %+v (one-based input converted exactly once)", looker.gotPos, want)
	}
	if !looker.gotInclude {
		t.Error("Lookup includeDeclaration = false, want true")
	}
	if looker.gotLang != "go" {
		t.Errorf("Lookup language = %q, want %q", looker.gotLang, "go")
	}
	if looker.calls != 2 {
		t.Errorf("Lookup calls = %d, want 2 (readiness stability requires two agreeing lookups)", looker.calls)
	}
	if out.Readiness != "stable" {
		t.Errorf("Readiness = %q, want %q", out.Readiness, "stable")
	}
}

func TestFindReferencesHandlerOneBasedCanonicalOutput(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	looker := &fakeRefLooker{
		refs: []lsp.NavigationLocation{
			{
				TargetURI:   "file:///workspace/z.go",
				TargetRange: lsp.NavigationRange{StartLine: 12, StartColumn: 1, EndLine: 12, EndColumn: 8},
			},
			{
				TargetURI:   "file:///workspace/a.go",
				TargetRange: lsp.NavigationRange{StartLine: 4, StartColumn: 8, EndLine: 4, EndColumn: 15},
			},
		},
	}
	handler := referencesHandler(looker, t.TempDir(), testResolver(t, "go", "python", "rust"))

	_, out, err := handler(t.Context(), nil, ReferencesInput{File: path, Line: 1, Column: 1, Language: "go"})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	want := []ReferenceItem{
		{
			URI:   "file:///workspace/a.go",
			Range: DefinitionRangeItem{StartLine: 5, StartColumn: 9, EndLine: 5, EndColumn: 16},
		},
		{
			URI:   "file:///workspace/z.go",
			Range: DefinitionRangeItem{StartLine: 13, StartColumn: 2, EndLine: 13, EndColumn: 9},
		},
	}
	if diff := gocmp.Diff(want, out.References); diff != "" {
		t.Errorf("references mismatch (-want +got):\n%s", diff)
	}
	if out.Readiness != "stable" {
		t.Errorf("Readiness = %q, want %q", out.Readiness, "stable")
	}
	if out.File != path {
		t.Errorf("File = %q, want %q", out.File, path)
	}
}

func TestFindReferencesHandlerEmptyResult(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	looker := &fakeRefLooker{}
	handler := referencesHandler(looker, t.TempDir(), testResolver(t, "go", "python", "rust"))

	_, out, err := handler(t.Context(), nil, ReferencesInput{File: path, Line: 1, Column: 1, Language: "go"})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if len(out.References) != 0 {
		t.Errorf("References = %d items, want 0: %+v", len(out.References), out.References)
	}
	if out.Readiness != "stable" {
		t.Errorf("Readiness = %q, want %q (two agreeing empty lookups are a trustworthy zero)", out.Readiness, "stable")
	}
}

func TestFindReferencesHandlerRetriesThroughTransientError(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	looker := &fakeRefLooker{
		err:     errors.New("still indexing"),
		errOnce: true,
		refs: []lsp.NavigationLocation{
			{
				TargetURI:   "file:///workspace/a.go",
				TargetRange: lsp.NavigationRange{StartLine: 4, StartColumn: 8, EndLine: 4, EndColumn: 15},
			},
		},
	}
	handler := referencesHandler(looker, t.TempDir(), testResolver(t, "go", "python", "rust"))

	_, out, err := handler(t.Context(), nil, ReferencesInput{File: path, Line: 1, Column: 1, Language: "go"})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if looker.calls != 3 {
		t.Errorf("Lookup calls = %d, want 3 (one failed attempt, then two agreeing)", looker.calls)
	}
	if len(out.References) != 1 {
		t.Errorf("References = %d items, want 1", len(out.References))
	}
	if out.Readiness != "stable" {
		t.Errorf("Readiness = %q, want %q", out.Readiness, "stable")
	}
}

func TestFindReferencesHandlerUnsupportedFailsFast(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	looker := &fakeRefLooker{err: fmt.Errorf("references request: %w", lsp.ErrUnsupported)}
	handler := referencesHandler(looker, t.TempDir(), testResolver(t, "go", "python", "rust"))

	_, _, err := handler(t.Context(), nil, ReferencesInput{File: path, Line: 1, Column: 1, Language: "go"})
	if !errors.Is(err, lsp.ErrUnsupported) {
		t.Fatalf("handler error = %v, want errors.Is lsp.ErrUnsupported", err)
	}
	if looker.calls != 1 {
		t.Errorf("Lookup calls = %d, want 1 (capability absence must not be retried)", looker.calls)
	}
}
