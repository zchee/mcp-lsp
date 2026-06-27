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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

type fakeHoverLooker struct {
	hover   *lsp.HoverResult
	gotLang string
	gotPath string
	gotText string
	gotPos  protocol.Position
	calls   int
}

func (f *fakeHoverLooker) Lookup(_ context.Context, lang, absPath, text string, pos protocol.Position) (*lsp.HoverResult, error) {
	f.calls++
	f.gotLang = lang
	f.gotPath = absPath
	f.gotText = text
	f.gotPos = pos
	return f.hover, nil
}

type fakeWorkspaceSymbolLooker struct {
	symbols  []lsp.WorkspaceSymbol
	gotLang  string
	gotQuery string
	calls    int
}

func (f *fakeWorkspaceSymbolLooker) Lookup(_ context.Context, lang, query string) ([]lsp.WorkspaceSymbol, error) {
	f.calls++
	f.gotLang = lang
	f.gotQuery = query
	return f.symbols, nil
}

type fakeFormatter struct {
	formatEdit      lsp.WorkspaceEdit
	rangeEdit       lsp.WorkspaceEdit
	formatCalls     int
	rangeCalls      int
	gotFormatLang   string
	gotFormatPath   string
	gotFormatText   string
	gotFormatOpts   protocol.FormattingOptions
	gotRangeLang    string
	gotRangePath    string
	gotRangeText    string
	gotRange        protocol.Range
	gotRangeOptions protocol.FormattingOptions
}

func (f *fakeFormatter) Format(_ context.Context, lang, absPath, text string, options protocol.FormattingOptions) (lsp.WorkspaceEdit, error) {
	f.formatCalls++
	f.gotFormatLang = lang
	f.gotFormatPath = absPath
	f.gotFormatText = text
	f.gotFormatOpts = options
	return f.formatEdit, nil
}

func (f *fakeFormatter) RangeFormat(_ context.Context, lang, absPath, text string, rng protocol.Range, options protocol.FormattingOptions) (lsp.WorkspaceEdit, error) {
	f.rangeCalls++
	f.gotRangeLang = lang
	f.gotRangePath = absPath
	f.gotRangeText = text
	f.gotRange = rng
	f.gotRangeOptions = options
	return f.rangeEdit, nil
}

type fakeRenamer struct {
	edit       lsp.WorkspaceEdit
	gotLang    string
	gotPath    string
	gotText    string
	gotPos     protocol.Position
	gotNewName string
	calls      int
}

func (f *fakeRenamer) Preview(_ context.Context, lang, absPath, text string, pos protocol.Position, newName string) (lsp.WorkspaceEdit, error) {
	f.calls++
	f.gotLang = lang
	f.gotPath = absPath
	f.gotText = text
	f.gotPos = pos
	f.gotNewName = newName
	return f.edit, nil
}

type fakeCodeActionLooker struct {
	actions    []lsp.CodeAction
	gotLang    string
	gotPath    string
	gotText    string
	gotRange   protocol.Range
	gotOnly    []protocol.CodeActionKind
	gotResolve bool
	calls      int
}

func (f *fakeCodeActionLooker) Lookup(_ context.Context, lang, absPath, text string, rng protocol.Range, only []protocol.CodeActionKind, resolve bool) ([]lsp.CodeAction, error) {
	f.calls++
	f.gotLang = lang
	f.gotPath = absPath
	f.gotText = text
	f.gotRange = rng
	f.gotOnly = append([]protocol.CodeActionKind(nil), only...)
	f.gotResolve = resolve
	return f.actions, nil
}

type fakeCodeLensLooker struct {
	lenses     []lsp.CodeLens
	gotLang    string
	gotPath    string
	gotText    string
	gotResolve bool
	calls      int
}

func (f *fakeCodeLensLooker) Lookup(_ context.Context, lang, absPath, text string, resolve bool) ([]lsp.CodeLens, error) {
	f.calls++
	f.gotLang = lang
	f.gotPath = absPath
	f.gotText = text
	f.gotResolve = resolve
	return f.lenses, nil
}

type fakeCommandExecutor struct {
	result        protocol.LSPAny
	gotLang       string
	gotCommand    string
	gotArgs       []protocol.LSPAny
	gotApplyEdits bool
	calls         int
}

func (f *fakeCommandExecutor) Execute(_ context.Context, lang, command string, args []protocol.LSPAny, applyEdits bool) (protocol.LSPAny, error) {
	f.calls++
	f.gotLang = lang
	f.gotCommand = command
	f.gotArgs = append([]protocol.LSPAny(nil), args...)
	f.gotApplyEdits = applyEdits
	return f.result, nil
}

func TestHoverHandlerReadsFileDefaultsLanguageAndConvertsCoordinates(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	hoverRange := lsp.NavigationRange{StartLine: 1, StartColumn: 2, EndLine: 1, EndColumn: 5}
	looker := &fakeHoverLooker{
		hover: &lsp.HoverResult{Kind: "markdown", Value: "**doc**", Range: &hoverRange},
	}
	handler := hoverHandler(looker, t.TempDir())

	_, out, err := handler(t.Context(), nil, HoverInput{File: path, Line: 3, Column: 5})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if looker.calls != 1 {
		t.Fatalf("Lookup calls = %d, want 1", looker.calls)
	}
	if looker.gotLang != "go" {
		t.Fatalf("Lookup language = %q, want go", looker.gotLang)
	}
	if looker.gotPath != path {
		t.Fatalf("Lookup path = %q, want %q", looker.gotPath, path)
	}
	if looker.gotText != fileContent {
		t.Fatalf("Lookup text = %q, want file contents", looker.gotText)
	}
	if want := (protocol.Position{Line: 2, Character: 4}); looker.gotPos != want {
		t.Fatalf("Lookup position = %+v, want %+v", looker.gotPos, want)
	}

	want := HoverOutput{
		File: path,
		URI:  string(uri.File(path)),
		Hover: &HoverItem{
			Kind:  "markdown",
			Value: "**doc**",
			Range: &DefinitionRangeItem{StartLine: 2, StartColumn: 3, EndLine: 2, EndColumn: 6},
		},
	}
	if diff := cmp.Diff(want, out); diff != "" {
		t.Fatalf("hover output mismatch (-want +got):\n%s", diff)
	}
}

func TestWorkspaceSymbolHandlerDefaultsLanguageAndConvertsRanges(t *testing.T) {
	t.Parallel()

	symbolRange := lsp.NavigationRange{StartLine: 4, StartColumn: 1, EndLine: 4, EndColumn: 8}
	looker := &fakeWorkspaceSymbolLooker{
		symbols: []lsp.WorkspaceSymbol{
			{Name: "Handler", Kind: "12", ContainerName: "server", URI: "file:///workspace/server.go", Range: &symbolRange},
			{Name: "pkg", Kind: "4", URI: "file:///workspace/pkg"},
		},
	}
	handler := workspaceSymbolHandler(looker)

	_, out, err := handler(t.Context(), nil, WorkspaceSymbolInput{Query: "handler"})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if looker.gotLang != "go" {
		t.Fatalf("Lookup language = %q, want go", looker.gotLang)
	}
	if looker.gotQuery != "handler" {
		t.Fatalf("Lookup query = %q, want handler", looker.gotQuery)
	}

	want := WorkspaceSymbolOutput{Symbols: []WorkspaceSymbolItem{
		{
			Name:          "Handler",
			Kind:          "12",
			ContainerName: "server",
			URI:           "file:///workspace/server.go",
			Range:         &DefinitionRangeItem{StartLine: 5, StartColumn: 2, EndLine: 5, EndColumn: 9},
		},
		{Name: "pkg", Kind: "4", URI: "file:///workspace/pkg"},
	}}
	if diff := cmp.Diff(want, out); diff != "" {
		t.Fatalf("workspace symbol output mismatch (-want +got):\n%s", diff)
	}
}

func TestFormattingHandlersReadFilesAndConvertInputs(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	path := filepath.Join(workspace, "main.go")
	writeFile(t, path, fileContent)
	fileURI := uri.File(path)
	insertSpaces := false
	formatter := &fakeFormatter{
		formatEdit: lsp.WorkspaceEdit{Changes: map[string][]lsp.WorkspaceTextEdit{
			fileURI.String(): {{Range: lsp.NavigationRange{StartLine: 0, StartColumn: 0, EndLine: 0, EndColumn: 0}, NewText: "// formatted\n"}},
		}},
		rangeEdit: lsp.WorkspaceEdit{Changes: map[string][]lsp.WorkspaceTextEdit{
			fileURI.String(): {{Range: lsp.NavigationRange{StartLine: 1, StartColumn: 0, EndLine: 1, EndColumn: 7}, NewText: "renamed"}},
		}},
	}

	formatHandler := formattingHandler(formatter, workspace)
	_, formatOut, err := formatHandler(t.Context(), nil, FormattingInput{File: "main.go", Language: "rust", TabSize: 2, InsertSpaces: &insertSpaces})
	if err != nil {
		t.Fatalf("formatting handler: %v", err)
	}
	if formatter.gotFormatLang != "rust" {
		t.Fatalf("Format language = %q, want rust", formatter.gotFormatLang)
	}
	if formatter.gotFormatPath != path || formatter.gotFormatText != fileContent {
		t.Fatalf("Format path/text = %q/%q, want %q/file contents", formatter.gotFormatPath, formatter.gotFormatText, path)
	}
	wantFormatOptions := protocol.FormattingOptions{TabSize: 2, InsertSpaces: false}
	if diff := cmp.Diff(wantFormatOptions, formatter.gotFormatOpts); diff != "" {
		t.Fatalf("Format options mismatch (-want +got):\n%s", diff)
	}
	wantFormatEdit := protocol.WorkspaceEdit{Changes: map[uri.URI][]protocol.TextEdit{
		fileURI: {{Range: protocol.Range{Start: protocol.Position{}, End: protocol.Position{}}, NewText: "// formatted\n"}},
	}}
	if diff := cmp.Diff(WorkspaceEditPreviewOutput{File: path, URI: fileURI.String(), Edit: wantFormatEdit}, formatOut); diff != "" {
		t.Fatalf("formatting output mismatch (-want +got):\n%s", diff)
	}

	rangeHandler := rangeFormattingHandler(formatter, workspace)
	_, rangeOut, err := rangeHandler(t.Context(), nil, RangeFormattingInput{
		FormattingInput: FormattingInput{File: "main.go"},
		StartLine:       2,
		StartColumn:     1,
		EndLine:         2,
		EndColumn:       8,
	})
	if err != nil {
		t.Fatalf("range formatting handler: %v", err)
	}
	wantRange := protocol.Range{Start: protocol.Position{Line: 1, Character: 0}, End: protocol.Position{Line: 1, Character: 7}}
	if formatter.gotRange != wantRange {
		t.Fatalf("RangeFormat range = %+v, want %+v", formatter.gotRange, wantRange)
	}
	wantDefaultOptions := protocol.FormattingOptions{TabSize: 4, InsertSpaces: true}
	if diff := cmp.Diff(wantDefaultOptions, formatter.gotRangeOptions); diff != "" {
		t.Fatalf("RangeFormat options mismatch (-want +got):\n%s", diff)
	}
	wantRangeEdit := protocol.WorkspaceEdit{Changes: map[uri.URI][]protocol.TextEdit{
		fileURI: {{Range: wantRange, NewText: "renamed"}},
	}}
	if diff := cmp.Diff(WorkspaceEditPreviewOutput{File: path, URI: fileURI.String(), Edit: wantRangeEdit}, rangeOut); diff != "" {
		t.Fatalf("range formatting output mismatch (-want +got):\n%s", diff)
	}
}

func TestRangeFormattingAndCodeActionValidateRangeBeforeFileIO(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "missing.go")

	formatter := &fakeFormatter{}
	rangeHandler := rangeFormattingHandler(formatter, t.TempDir())
	_, _, err := rangeHandler(t.Context(), nil, RangeFormattingInput{
		FormattingInput: FormattingInput{File: missing},
		StartLine:       0,
		StartColumn:     1,
		EndLine:         1,
		EndColumn:       1,
	})
	if err == nil || !strings.Contains(err.Error(), "line must be greater than zero") {
		t.Fatalf("range formatting error = %v, want line validation error", err)
	}
	if formatter.rangeCalls != 0 {
		t.Fatalf("RangeFormat calls = %d, want 0", formatter.rangeCalls)
	}

	codeActions := &fakeCodeActionLooker{}
	codeActionHandler := codeActionHandler(codeActions, t.TempDir())
	_, _, err = codeActionHandler(t.Context(), nil, CodeActionInput{
		File:        missing,
		StartLine:   1,
		StartColumn: 0,
		EndLine:     1,
		EndColumn:   1,
	})
	if err == nil || !strings.Contains(err.Error(), "column must be greater than zero") {
		t.Fatalf("code action error = %v, want column validation error", err)
	}
	if codeActions.calls != 0 {
		t.Fatalf("CodeAction calls = %d, want 0", codeActions.calls)
	}
}

func TestRenameHandlerValidatesAndReturnsWorkspaceEditPreview(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	fileURI := uri.File(path)
	renamer := &fakeRenamer{
		edit: lsp.WorkspaceEdit{Changes: map[string][]lsp.WorkspaceTextEdit{
			fileURI.String(): {{Range: lsp.NavigationRange{StartLine: 2, StartColumn: 4, EndLine: 2, EndColumn: 8}, NewText: "Renamed"}},
		}},
	}
	handler := renameHandler(renamer, t.TempDir())

	_, _, err := handler(t.Context(), nil, RenameInput{File: path, Line: 3, Column: 5})
	if err == nil || !strings.Contains(err.Error(), "newName is required") {
		t.Fatalf("empty newName error = %v, want required error", err)
	}
	if renamer.calls != 0 {
		t.Fatalf("Preview calls = %d, want 0 for invalid input", renamer.calls)
	}

	_, out, err := handler(t.Context(), nil, RenameInput{File: path, Line: 3, Column: 5, NewName: "Renamed"})
	if err != nil {
		t.Fatalf("rename handler: %v", err)
	}
	if renamer.gotLang != "go" {
		t.Fatalf("Preview language = %q, want go", renamer.gotLang)
	}
	if want := (protocol.Position{Line: 2, Character: 4}); renamer.gotPos != want {
		t.Fatalf("Preview position = %+v, want %+v", renamer.gotPos, want)
	}
	if renamer.gotNewName != "Renamed" {
		t.Fatalf("Preview newName = %q, want Renamed", renamer.gotNewName)
	}
	wantEdit := protocol.WorkspaceEdit{Changes: map[uri.URI][]protocol.TextEdit{
		fileURI: {{Range: protocol.Range{Start: protocol.Position{Line: 2, Character: 4}, End: protocol.Position{Line: 2, Character: 8}}, NewText: "Renamed"}},
	}}
	if diff := cmp.Diff(WorkspaceEditPreviewOutput{File: path, URI: fileURI.String(), Edit: wantEdit}, out); diff != "" {
		t.Fatalf("rename output mismatch (-want +got):\n%s", diff)
	}
}

func TestCodeActionHandlerConvertsRangeKindsAndEdits(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	fileURI := uri.File(path)
	isPreferred := true
	edit := lsp.WorkspaceEdit{Changes: map[string][]lsp.WorkspaceTextEdit{
		fileURI.String(): {{Range: lsp.NavigationRange{StartLine: 0, StartColumn: 0, EndLine: 0, EndColumn: 4}, NewText: "fixed"}},
	}}
	looker := &fakeCodeActionLooker{
		actions: []lsp.CodeAction{
			{
				Title:       "Fix",
				Kind:        string(protocol.CodeActionKindQuickFix),
				IsPreferred: &isPreferred,
				Edit:        &edit,
				Command:     &lsp.Command{Title: "Apply", Tooltip: "apply edit", Command: "server.apply"},
			},
		},
	}
	handler := codeActionHandler(looker, t.TempDir())

	_, out, err := handler(t.Context(), nil, CodeActionInput{
		File:        path,
		StartLine:   1,
		StartColumn: 1,
		EndLine:     1,
		EndColumn:   5,
		Only:        []string{string(protocol.CodeActionKindQuickFix)},
		Resolve:     true,
	})
	if err != nil {
		t.Fatalf("code action handler: %v", err)
	}
	wantRange := protocol.Range{Start: protocol.Position{Line: 0, Character: 0}, End: protocol.Position{Line: 0, Character: 4}}
	if looker.gotRange != wantRange {
		t.Fatalf("Lookup range = %+v, want %+v", looker.gotRange, wantRange)
	}
	if diff := cmp.Diff([]protocol.CodeActionKind{protocol.CodeActionKindQuickFix}, looker.gotOnly); diff != "" {
		t.Fatalf("Lookup only mismatch (-want +got):\n%s", diff)
	}
	if !looker.gotResolve {
		t.Fatal("Lookup resolve = false, want true")
	}
	wantEdit := protocol.WorkspaceEdit{Changes: map[uri.URI][]protocol.TextEdit{
		fileURI: {{Range: wantRange, NewText: "fixed"}},
	}}
	want := CodeActionOutput{
		File: path,
		URI:  fileURI.String(),
		Actions: []CodeActionItem{
			{
				Title:       "Fix",
				Kind:        string(protocol.CodeActionKindQuickFix),
				IsPreferred: &isPreferred,
				Edit:        &wantEdit,
				Command:     &CommandItem{Title: "Apply", Tooltip: "apply edit", Command: "server.apply"},
			},
		},
	}
	if diff := cmp.Diff(want, out); diff != "" {
		t.Fatalf("code action output mismatch (-want +got):\n%s", diff)
	}
}

func TestCodeLensHandlerReadsFileAndConvertsOutputRanges(t *testing.T) {
	t.Parallel()

	path := writeTempFile(t)
	looker := &fakeCodeLensLooker{
		lenses: []lsp.CodeLens{
			{
				Range:   lsp.NavigationRange{StartLine: 4, StartColumn: 0, EndLine: 4, EndColumn: 0},
				Command: &lsp.Command{Title: "Test", Command: "go.test"},
			},
		},
	}
	handler := codeLensHandler(looker, t.TempDir())

	_, out, err := handler(t.Context(), nil, CodeLensInput{File: path, Resolve: true})
	if err != nil {
		t.Fatalf("code lens handler: %v", err)
	}
	if looker.gotLang != "go" || looker.gotPath != path || looker.gotText != fileContent || !looker.gotResolve {
		t.Fatalf("Lookup args = lang:%q path:%q text:%q resolve:%v", looker.gotLang, looker.gotPath, looker.gotText, looker.gotResolve)
	}
	want := CodeLensOutput{
		File: path,
		URI:  uri.File(path).String(),
		Lenses: []CodeLensItem{
			{
				Range:   DefinitionRangeItem{StartLine: 5, StartColumn: 1, EndLine: 5, EndColumn: 1},
				Command: &CommandItem{Title: "Test", Command: "go.test"},
			},
		},
	}
	if diff := cmp.Diff(want, out); diff != "" {
		t.Fatalf("code lens output mismatch (-want +got):\n%s", diff)
	}
}

func TestExecuteCommandHandlerDefaultsLanguageAndKeepsRawArguments(t *testing.T) {
	t.Parallel()

	executor := &fakeCommandExecutor{result: protocol.LSPAny(`{"ok":true}`)}
	handler := executeCommandHandler(executor)

	_, _, err := handler(t.Context(), nil, ExecuteCommandInput{})
	if err == nil || !strings.Contains(err.Error(), "command is required") {
		t.Fatalf("empty command error = %v, want required error", err)
	}
	if executor.calls != 0 {
		t.Fatalf("Execute calls = %d, want 0 for invalid input", executor.calls)
	}

	args := []protocol.LSPAny{protocol.LSPAny(`"arg"`), protocol.LSPAny(`1`)}
	_, out, err := handler(t.Context(), nil, ExecuteCommandInput{Command: "server.test", Arguments: args, ApplyEdits: true})
	if err != nil {
		t.Fatalf("execute command handler: %v", err)
	}
	if executor.gotLang != "go" {
		t.Fatalf("Execute language = %q, want go", executor.gotLang)
	}
	if executor.gotCommand != "server.test" || !executor.gotApplyEdits {
		t.Fatalf("Execute command/applyEdits = %q/%v, want server.test/true", executor.gotCommand, executor.gotApplyEdits)
	}
	if len(executor.gotArgs) != 2 || string(executor.gotArgs[0]) != `"arg"` || string(executor.gotArgs[1]) != `1` {
		t.Fatalf("Execute arguments = %v, want raw JSON args", executor.gotArgs)
	}
	if string(out.Result) != `{"ok":true}` {
		t.Fatalf("Execute result = %s, want {\"ok\":true}", out.Result)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
}
