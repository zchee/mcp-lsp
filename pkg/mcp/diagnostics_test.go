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
	"testing"

	gocmp "github.com/google/go-cmp/cmp"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

// fakeLooker is a diagLooker test double recording its arguments and returning a
// canned result or error.
type fakeLooker struct {
	diags   []lsp.Diagnostic
	err     error
	gotLang string
	gotPath string
	gotText string
	calls   int
}

func (f *fakeLooker) Lookup(_ context.Context, lang, absPath, text string) ([]lsp.Diagnostic, error) {
	f.calls++
	f.gotLang = lang
	f.gotPath = absPath
	f.gotText = text
	if f.err != nil {
		return nil, f.err
	}
	return f.diags, nil
}

// fileContent is the source written to the temporary file used by the handler
// tests; its bytes are forwarded verbatim to the fake looker.
const fileContent = "package main\n"

// writeTempFile writes fileContent to a file in a temp dir and returns its path.
func writeTempFile(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(path, []byte(fileContent), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func TestDiagnosticsHandlerEmptyFile(t *testing.T) {
	t.Parallel()

	looker := &fakeLooker{}
	handler := diagnosticsHandler(looker, t.TempDir())

	_, _, err := handler(t.Context(), nil, DiagnosticsInput{File: ""})
	if err == nil {
		t.Fatal("handler returned nil error for empty File")
	}
	if looker.calls != 0 {
		t.Errorf("handler called Lookup %d times for invalid input, want 0", looker.calls)
	}
}

func TestDiagnosticsHandlerDefaultsLanguage(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	looker := &fakeLooker{}
	handler := diagnosticsHandler(looker, t.TempDir())

	_, _, err := handler(t.Context(), nil, DiagnosticsInput{File: path})
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

func TestDiagnosticsHandlerResolvesRelativeFileFromWorkspaceRoot(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	path := filepath.Join(workspace, "main.go")
	if err := os.WriteFile(path, []byte(fileContent), 0o600); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}

	looker := &fakeLooker{}
	handler := diagnosticsHandler(looker, workspace)

	_, out, err := handler(t.Context(), nil, DiagnosticsInput{File: "main.go"})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if looker.gotPath != path {
		t.Errorf("Lookup path = %q, want workspace-relative path %q", looker.gotPath, path)
	}
	if out.File != path {
		t.Errorf("handler output File = %q, want %q", out.File, path)
	}
}

func TestDiagnosticsHandlerOneBasedConversion(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	looker := &fakeLooker{
		diags: []lsp.Diagnostic{
			{
				StartLine: 0, StartColumn: 0, EndLine: 0, EndColumn: 3,
				Severity: "error", Source: "compiler", Code: "E001",
				Message: "boom",
			},
		},
	}
	handler := diagnosticsHandler(looker, t.TempDir())

	_, out, err := handler(t.Context(), nil, DiagnosticsInput{File: path, Language: "go"})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	want := DiagnosticsOutput{
		File: path,
		Diagnostics: []DiagnosticItem{
			{
				Severity: "error", Message: "boom", Source: "compiler", Code: "E001",
				StartLine: 1, StartColumn: 1, EndLine: 1, EndColumn: 4,
			},
		},
	}
	if diff := gocmp.Diff(want, out); diff != "" {
		t.Errorf("handler output mismatch (-want +got):\n%s", diff)
	}
}

func TestDiagnosticsHandlerSurfacesLookupError(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	sentinel := errors.New("language server initialize failed")
	looker := &fakeLooker{err: sentinel}
	handler := diagnosticsHandler(looker, t.TempDir())

	_, _, err := handler(t.Context(), nil, DiagnosticsInput{File: path})
	if !errors.Is(err, sentinel) {
		t.Fatalf("handler error = %v, want it to wrap %v", err, sentinel)
	}
}

func TestDiagnosticsHandlerMissingFile(t *testing.T) {
	t.Parallel()

	looker := &fakeLooker{}
	handler := diagnosticsHandler(looker, t.TempDir())

	missing := filepath.Join(t.TempDir(), "does-not-exist.go")
	_, _, err := handler(t.Context(), nil, DiagnosticsInput{File: missing})
	if err == nil {
		t.Fatal("handler returned nil error for a missing file")
	}
	if looker.calls != 0 {
		t.Errorf("handler called Lookup %d times when the file could not be read, want 0", looker.calls)
	}
}

func TestDiagnosticsHandlerEmptyResult(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	looker := &fakeLooker{diags: nil}
	handler := diagnosticsHandler(looker, t.TempDir())

	_, out, err := handler(t.Context(), nil, DiagnosticsInput{File: path})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if len(out.Diagnostics) != 0 {
		t.Errorf("handler returned %d diagnostics for a clean file, want 0", len(out.Diagnostics))
	}
	if out.File != path {
		t.Errorf("handler output File = %q, want %q", out.File, path)
	}
}
