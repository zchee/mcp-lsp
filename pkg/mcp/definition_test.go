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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

// fakeDefLooker is a defLooker test double recording its arguments and
// returning a canned result or error.
type fakeDefLooker struct {
	defs    []lsp.DefinitionLocation
	err     error
	gotLang string
	gotPath string
	gotText string
	gotPos  protocol.Position
	calls   int
}

func (f *fakeDefLooker) Lookup(_ context.Context, lang, absPath, text string, pos protocol.Position) ([]lsp.DefinitionLocation, error) {
	f.calls++
	f.gotLang = lang
	f.gotPath = absPath
	f.gotText = text
	f.gotPos = pos
	if f.err != nil {
		return nil, f.err
	}

	return f.defs, nil
}

func TestDefinitionHandlerEmptyFile(t *testing.T) {
	t.Parallel()

	looker := &fakeDefLooker{}
	handler := definitionHandler(looker, t.TempDir())

	_, _, err := handler(t.Context(), nil, DefinitionInput{Line: 1, Column: 1})
	if err == nil || !strings.Contains(err.Error(), "file is required") {
		t.Fatalf("handler error = %v, want file required error", err)
	}
	if looker.calls != 0 {
		t.Errorf("handler called Lookup %d times for invalid input, want 0", looker.calls)
	}
}

func TestDefinitionHandlerInvalidLine(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	looker := &fakeDefLooker{}
	handler := definitionHandler(looker, t.TempDir())

	_, _, err := handler(t.Context(), nil, DefinitionInput{File: path, Line: 0, Column: 1})
	if err == nil || !strings.Contains(err.Error(), "line must be greater than zero") {
		t.Fatalf("handler error = %v, want invalid line error", err)
	}
	if looker.calls != 0 {
		t.Errorf("handler called Lookup %d times for invalid input, want 0", looker.calls)
	}
}

func TestDefinitionHandlerInvalidColumn(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	looker := &fakeDefLooker{}
	handler := definitionHandler(looker, t.TempDir())

	_, _, err := handler(t.Context(), nil, DefinitionInput{File: path, Line: 1, Column: 0})
	if err == nil || !strings.Contains(err.Error(), "column must be greater than zero") {
		t.Fatalf("handler error = %v, want invalid column error", err)
	}
	if looker.calls != 0 {
		t.Errorf("handler called Lookup %d times for invalid input, want 0", looker.calls)
	}
}

func TestDefinitionHandlerRejectsPositionBeyondProtocolRange(t *testing.T) {
	t.Parallel()

	if strconv.IntSize <= 32 {
		t.Skip("int cannot represent values beyond the LSP uint32 position range")
	}

	tooLarge64 := maxProtocolPositionInput
	tooLarge64++
	tooLarge := int(tooLarge64)
	path := writeTempFile(t)

	tests := []struct {
		name  string
		input DefinitionInput
		want  string
	}{
		{
			name:  "line",
			input: DefinitionInput{File: path, Line: tooLarge, Column: 1},
			want:  "line must be less than or equal to",
		},
		{
			name:  "column",
			input: DefinitionInput{File: path, Line: 1, Column: tooLarge},
			want:  "column must be less than or equal to",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			looker := &fakeDefLooker{}
			handler := definitionHandler(looker, t.TempDir())

			_, _, err := handler(t.Context(), nil, tt.input)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("handler error = %v, want %q", err, tt.want)
			}
			if looker.calls != 0 {
				t.Errorf("handler called Lookup %d times for invalid input, want 0", looker.calls)
			}
		})
	}
}

func TestDefinitionHandlerDefaultsLanguage(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	looker := &fakeDefLooker{}
	handler := definitionHandler(looker, t.TempDir())

	_, _, err := handler(t.Context(), nil, DefinitionInput{File: path, Line: 1, Column: 1})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if looker.gotLang != "go" {
		t.Errorf("Lookup language = %q, want %q (default)", looker.gotLang, "go")
	}
	if looker.gotText != fileContent {
		t.Errorf("Lookup text = %q, want the file contents", looker.gotText)
	}
	if looker.gotPath != path {
		t.Errorf("Lookup path = %q, want %q", looker.gotPath, path)
	}
}

func TestDefinitionHandlerResolvesRelativeFileFromWorkspaceRoot(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	path := filepath.Join(workspace, "main.go")
	if err := os.WriteFile(path, []byte(fileContent), 0o600); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}

	looker := &fakeDefLooker{}
	handler := definitionHandler(looker, workspace)

	_, out, err := handler(t.Context(), nil, DefinitionInput{File: "main.go", Line: 1, Column: 1})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if looker.gotPath != path {
		t.Errorf("Lookup path = %q, want workspace-relative path %q", looker.gotPath, path)
	}
	if out.File != path {
		t.Errorf("handler output File = %q, want %q", out.File, path)
	}
	if out.URI != string(uri.File(path)) {
		t.Errorf("handler output URI = %q, want %q", out.URI, uri.File(path))
	}
}

func TestDefinitionHandlerConvertsInputToZeroBased(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	looker := &fakeDefLooker{}
	handler := definitionHandler(looker, t.TempDir())

	_, _, err := handler(t.Context(), nil, DefinitionInput{File: path, Line: 3, Column: 5})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	want := protocol.Position{Line: 2, Character: 4}
	if looker.gotPos != want {
		t.Errorf("Lookup position = %+v, want %+v", looker.gotPos, want)
	}
}

func TestDefinitionHandlerOneBasedOutput(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	targetURI := string(uri.File("/workspace/lib.go"))
	looker := &fakeDefLooker{
		defs: []lsp.DefinitionLocation{
			{
				TargetURI:            targetURI,
				TargetRange:          lsp.DefinitionRange{StartLine: 10, StartColumn: 0, EndLine: 14, EndColumn: 1},
				TargetSelectionRange: lsp.DefinitionRange{StartLine: 10, StartColumn: 0, EndLine: 14, EndColumn: 1},
			},
		},
	}
	handler := definitionHandler(looker, t.TempDir())

	_, out, err := handler(t.Context(), nil, DefinitionInput{File: path, Line: 1, Column: 1, Language: "go"})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	want := DefinitionOutput{
		File: path,
		URI:  string(uri.File(path)),
		Definitions: []DefinitionItem{
			{
				TargetURI:            targetURI,
				TargetRange:          DefinitionRangeItem{StartLine: 11, StartColumn: 1, EndLine: 15, EndColumn: 2},
				TargetSelectionRange: DefinitionRangeItem{StartLine: 11, StartColumn: 1, EndLine: 15, EndColumn: 2},
			},
		},
	}
	if diff := cmp.Diff(want, out); diff != "" {
		t.Errorf("handler output mismatch (-want +got):\n%s", diff)
	}
}

func TestDefinitionHandlerDefinitionLinkOutput(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	targetURI := string(uri.File("/workspace/linked.go"))
	origin := lsp.DefinitionRange{StartLine: 1, StartColumn: 2, EndLine: 1, EndColumn: 8}
	looker := &fakeDefLooker{
		defs: []lsp.DefinitionLocation{
			{
				TargetURI:            targetURI,
				TargetRange:          lsp.DefinitionRange{StartLine: 10, StartColumn: 0, EndLine: 14, EndColumn: 1},
				TargetSelectionRange: lsp.DefinitionRange{StartLine: 11, StartColumn: 4, EndLine: 11, EndColumn: 10},
				OriginSelectionRange: &origin,
			},
		},
	}
	handler := definitionHandler(looker, t.TempDir())

	_, out, err := handler(t.Context(), nil, DefinitionInput{File: path, Line: 1, Column: 1, Language: "go"})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	want := DefinitionOutput{
		File: path,
		URI:  string(uri.File(path)),
		Definitions: []DefinitionItem{
			{
				TargetURI:            targetURI,
				TargetRange:          DefinitionRangeItem{StartLine: 11, StartColumn: 1, EndLine: 15, EndColumn: 2},
				TargetSelectionRange: DefinitionRangeItem{StartLine: 12, StartColumn: 5, EndLine: 12, EndColumn: 11},
				OriginSelectionRange: &DefinitionRangeItem{StartLine: 2, StartColumn: 3, EndLine: 2, EndColumn: 9},
			},
		},
	}
	if diff := cmp.Diff(want, out); diff != "" {
		t.Errorf("handler output mismatch (-want +got):\n%s", diff)
	}
}

func TestDefinitionHandlerEmptyResult(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	looker := &fakeDefLooker{}
	handler := definitionHandler(looker, t.TempDir())

	_, out, err := handler(t.Context(), nil, DefinitionInput{File: path, Line: 1, Column: 1})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if out.Definitions == nil {
		t.Fatal("handler returned nil definitions slice, want empty slice")
	}
	if len(out.Definitions) != 0 {
		t.Errorf("handler returned %d definitions for an empty result, want 0", len(out.Definitions))
	}
}

func TestDefinitionHandlerSurfacesLookupError(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	sentinel := errors.New("language server initialize failed")
	looker := &fakeDefLooker{err: sentinel}
	handler := definitionHandler(looker, t.TempDir())

	_, _, err := handler(t.Context(), nil, DefinitionInput{File: path, Line: 1, Column: 1})
	if !errors.Is(err, sentinel) {
		t.Fatalf("handler error = %v, want it to wrap %v", err, sentinel)
	}
}

func TestDefinitionHandlerMissingFile(t *testing.T) {
	t.Parallel()

	looker := &fakeDefLooker{}
	handler := definitionHandler(looker, t.TempDir())

	missing := filepath.Join(t.TempDir(), "does-not-exist.go")
	_, _, err := handler(t.Context(), nil, DefinitionInput{File: missing, Line: 1, Column: 1})
	if err == nil {
		t.Fatal("handler returned nil error for a missing file")
	}
	if looker.calls != 0 {
		t.Errorf("handler called Lookup %d times when the file could not be read, want 0", looker.calls)
	}
}
