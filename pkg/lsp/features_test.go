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

package lsp

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/go-json-experiment/json"
	gocmp "github.com/google/go-cmp/cmp"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

type featureServer struct {
	protocol.UnimplementedServer

	mu           sync.Mutex
	capabilities protocol.ServerCapabilities

	opened  []protocol.DidOpenTextDocumentParams
	changed []protocol.DidChangeTextDocumentParams
	closed  []protocol.DidCloseTextDocumentParams

	hoverResult   *protocol.Hover
	hoverRequests []protocol.HoverParams

	symbolResult   protocol.WorkspaceSymbolResult
	symbolRequests []protocol.WorkspaceSymbolParams

	formattingEdits    []protocol.TextEdit
	formattingRequests []protocol.DocumentFormattingParams

	rangeFormattingEdits    []protocol.TextEdit
	rangeFormattingRequests []protocol.DocumentRangeFormattingParams

	renameEdit     *protocol.WorkspaceEdit
	renameRequests []protocol.RenameParams

	codeActions               []protocol.CommandOrCodeAction
	codeActionRequests        []protocol.CodeActionParams
	codeActionResolveResult   *protocol.CodeAction
	codeActionResolveRequests []protocol.CodeAction

	codeLenses              []protocol.CodeLens
	codeLensRequests        []protocol.CodeLensParams
	codeLensResolveResult   *protocol.CodeLens
	codeLensResolveRequests []protocol.CodeLens

	executeResult   protocol.LSPAny
	executeRequests []protocol.ExecuteCommandParams
}

func (f *featureServer) Initialize(_ context.Context, _ *protocol.InitializeParams) (*protocol.InitializeResult, error) {
	f.mu.Lock()
	capabilities := f.capabilities
	f.mu.Unlock()

	return &protocol.InitializeResult{
		ServerInfo:   protocol.ServerInfo{Name: "feature-fake"},
		Capabilities: capabilities,
	}, nil
}

func (f *featureServer) DidOpen(_ context.Context, params *protocol.DidOpenTextDocumentParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.opened = append(f.opened, *params)
	return nil
}

func (f *featureServer) DidChange(_ context.Context, params *protocol.DidChangeTextDocumentParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.changed = append(f.changed, *params)
	return nil
}

func (f *featureServer) DidClose(_ context.Context, params *protocol.DidCloseTextDocumentParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.closed = append(f.closed, *params)
	return nil
}

func (f *featureServer) Hover(_ context.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.hoverRequests = append(f.hoverRequests, *params)
	return f.hoverResult, nil
}

func (f *featureServer) Symbols(_ context.Context, params *protocol.WorkspaceSymbolParams) (protocol.WorkspaceSymbolResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.symbolRequests = append(f.symbolRequests, *params)
	return f.symbolResult, nil
}

func (f *featureServer) Formatting(_ context.Context, params *protocol.DocumentFormattingParams) ([]protocol.TextEdit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.formattingRequests = append(f.formattingRequests, *params)
	return slices.Clone(f.formattingEdits), nil
}

func (f *featureServer) RangeFormatting(_ context.Context, params *protocol.DocumentRangeFormattingParams) ([]protocol.TextEdit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.rangeFormattingRequests = append(f.rangeFormattingRequests, *params)
	return slices.Clone(f.rangeFormattingEdits), nil
}

func (f *featureServer) Rename(_ context.Context, params *protocol.RenameParams) (*protocol.WorkspaceEdit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.renameRequests = append(f.renameRequests, *params)
	return f.renameEdit, nil
}

func (f *featureServer) CodeAction(_ context.Context, params *protocol.CodeActionParams) ([]protocol.CommandOrCodeAction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.codeActionRequests = append(f.codeActionRequests, *params)
	return slices.Clone(f.codeActions), nil
}

func (f *featureServer) CodeActionResolve(_ context.Context, params *protocol.CodeAction) (*protocol.CodeAction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.codeActionResolveRequests = append(f.codeActionResolveRequests, *params)
	return f.codeActionResolveResult, nil
}

func (f *featureServer) CodeLens(_ context.Context, params *protocol.CodeLensParams) ([]protocol.CodeLens, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.codeLensRequests = append(f.codeLensRequests, *params)
	return slices.Clone(f.codeLenses), nil
}

func (f *featureServer) CodeLensResolve(_ context.Context, params *protocol.CodeLens) (*protocol.CodeLens, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.codeLensResolveRequests = append(f.codeLensResolveRequests, *params)
	return f.codeLensResolveResult, nil
}

func (f *featureServer) ExecuteCommand(_ context.Context, params *protocol.ExecuteCommandParams) (protocol.LSPAny, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.executeRequests = append(f.executeRequests, *params)
	return f.executeResult, nil
}

func (f *featureServer) openedDocs() []protocol.DidOpenTextDocumentParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.opened)
}

func (f *featureServer) changedDocs() []protocol.DidChangeTextDocumentParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.changed)
}

func (f *featureServer) hoverCalls() []protocol.HoverParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.hoverRequests)
}

func (f *featureServer) symbolCalls() []protocol.WorkspaceSymbolParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.symbolRequests)
}

func (f *featureServer) formattingCalls() []protocol.DocumentFormattingParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.formattingRequests)
}

func (f *featureServer) rangeFormattingCalls() []protocol.DocumentRangeFormattingParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.rangeFormattingRequests)
}

func (f *featureServer) renameCalls() []protocol.RenameParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.renameRequests)
}

func (f *featureServer) codeActionCalls() []protocol.CodeActionParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.codeActionRequests)
}

func (f *featureServer) codeActionResolveCalls() []protocol.CodeAction {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.codeActionResolveRequests)
}

func (f *featureServer) codeLensCalls() []protocol.CodeLensParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.codeLensRequests)
}

func (f *featureServer) codeLensResolveCalls() []protocol.CodeLens {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.codeLensResolveRequests)
}

func (f *featureServer) executeCalls() []protocol.ExecuteCommandParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.executeRequests)
}

func newFeatureManager(t *testing.T, srv *featureServer, rootDir string) *Manager {
	t.Helper()

	sess, _ := wireSessionCore(t, srv)
	return &Manager{
		cfg: map[string]ServerConfig{
			"go": {LanguageID: protocol.LanguageKindGo},
		},
		sessions: map[string]*serverSession{"go": sess},
		rootDir:  rootDir,
		logger:   slog.New(slog.DiscardHandler),
	}
}

func requireErrorContains(t *testing.T, err error, want string) {
	t.Helper()

	if err == nil {
		t.Fatalf("error = nil, want contains %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want contains %q", err, want)
	}
}

func TestFeatureLookupsRejectUnsupportedCapabilitiesBeforeSync(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	srv := &featureServer{}
	mgr := newFeatureManager(t, srv, root)
	rng := protocol.Range{Start: protocol.Position{Line: 0, Character: 0}, End: protocol.Position{Line: 0, Character: 1}}

	tests := []struct {
		name string
		run  func() error
		want string
	}{
		{
			name: "hover",
			run: func() error {
				_, err := mgr.Hover().Lookup(t.Context(), "go", path, "package main\n", protocol.Position{})
				return err
			},
			want: "hover request is not supported",
		},
		{
			name: "workspace symbol",
			run: func() error {
				_, err := mgr.WorkspaceSymbols().Lookup(t.Context(), "go", "main")
				return err
			},
			want: "workspace/symbol request is not supported",
		},
		{
			name: "formatting",
			run: func() error {
				_, err := mgr.Formatting().Format(t.Context(), "go", path, "package main\n", protocol.FormattingOptions{})
				return err
			},
			want: "formatting request is not supported",
		},
		{
			name: "range formatting",
			run: func() error {
				_, err := mgr.Formatting().RangeFormat(t.Context(), "go", path, "package main\n", rng, protocol.FormattingOptions{})
				return err
			},
			want: "range formatting request is not supported",
		},
		{
			name: "rename",
			run: func() error {
				_, err := mgr.Rename().Preview(t.Context(), "go", path, "package main\n", protocol.Position{}, "renamed")
				return err
			},
			want: "rename request is not supported",
		},
		{
			name: "code action",
			run: func() error {
				_, err := mgr.CodeActions().Lookup(t.Context(), "go", path, "package main\n", rng, nil, false)
				return err
			},
			want: "code action request is not supported",
		},
		{
			name: "code lens",
			run: func() error {
				_, err := mgr.CodeLenses().Lookup(t.Context(), "go", path, "package main\n", false)
				return err
			},
			want: "code lens request is not supported",
		},
		{
			name: "execute command",
			run: func() error {
				_, err := mgr.Commands().Execute(t.Context(), "go", "missing", nil, false)
				return err
			},
			want: `execute command "missing" is not advertised`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireErrorContains(t, tt.run(), tt.want)
		})
	}

	if got := len(srv.openedDocs()); got != 0 {
		t.Fatalf("unsupported feature calls opened %d documents, want 0", got)
	}
	if got := len(srv.changedDocs()); got != 0 {
		t.Fatalf("unsupported feature calls changed %d documents, want 0", got)
	}
}

func TestHoverLookupFlattensMarkupAndRecordsWireParams(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	hoverRange := protocol.Range{
		Start: protocol.Position{Line: 1, Character: 2},
		End:   protocol.Position{Line: 1, Character: 5},
	}
	srv := &featureServer{
		capabilities: protocol.ServerCapabilities{HoverProvider: protocol.Boolean(true)},
		hoverResult: &protocol.Hover{
			Contents: &protocol.MarkupContent{Kind: protocol.MarkupKindMarkdown, Value: "**doc**"},
			Range:    &hoverRange,
		},
	}
	mgr := newFeatureManager(t, srv, root)
	pos := protocol.Position{Line: 3, Character: 4}

	got, err := mgr.Hover().Lookup(t.Context(), "go", path, "package main\n", pos)
	if err != nil {
		t.Fatalf("Hover.Lookup: %v", err)
	}

	wantRange := NavigationRange{StartLine: 1, StartColumn: 2, EndLine: 1, EndColumn: 5}
	want := &HoverResult{Kind: "markdown", Value: "**doc**", Range: &wantRange}
	if diff := gocmp.Diff(want, got); diff != "" {
		t.Fatalf("hover result mismatch (-want +got):\n%s", diff)
	}

	calls := srv.hoverCalls()
	if len(calls) != 1 {
		t.Fatalf("hover calls = %d, want 1", len(calls))
	}
	if gotURI := calls[0].TextDocument.URI; gotURI != uri.File(path) {
		t.Fatalf("hover URI = %q, want %q", gotURI, uri.File(path))
	}
	if calls[0].Position != pos {
		t.Fatalf("hover position = %+v, want %+v", calls[0].Position, pos)
	}
	opened := srv.openedDocs()
	if len(opened) != 1 {
		t.Fatalf("didOpen calls = %d, want 1", len(opened))
	}
	if opened[0].TextDocument.LanguageID != protocol.LanguageKindGo {
		t.Fatalf("didOpen languageID = %q, want %q", opened[0].TextDocument.LanguageID, protocol.LanguageKindGo)
	}
}

func TestFlattenHoverSupportsLegacyMarkedStringWireShapes(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		raw  string
		want *HoverResult
	}{
		"marked string with language": {
			raw:  `{"contents":{"language":"go","value":"func main()"}}`,
			want: &HoverResult{Kind: "plaintext", Value: "```go\nfunc main()\n```"},
		},
		"marked string slice": {
			raw:  `{"contents":["plain",{"language":"go","value":"func main()"}]}`,
			want: &HoverResult{Kind: "plaintext", Value: "plain\n\n```go\nfunc main()\n```"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var hover protocol.Hover
			if err := json.Unmarshal([]byte(tt.raw), &hover); err != nil {
				t.Fatalf("unmarshal hover: %v", err)
			}
			if diff := gocmp.Diff(tt.want, flattenHover(&hover)); diff != "" {
				t.Fatalf("hover mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestWorkspaceSymbolsLookupFlattensResultUnions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result protocol.WorkspaceSymbolResult
		want   []WorkspaceSymbol
	}{
		{
			name: "symbol information",
			result: protocol.SymbolInformationSlice{
				{
					Name:          "Handler",
					Kind:          protocol.SymbolKindFunction,
					ContainerName: new("server"),
					Location: protocol.Location{
						URI: uri.File("/workspace/server.go"),
						Range: protocol.Range{
							Start: protocol.Position{Line: 10, Character: 2},
							End:   protocol.Position{Line: 10, Character: 9},
						},
					},
				},
			},
			want: []WorkspaceSymbol{
				{
					Name:          "Handler",
					Kind:          fmt.Sprint(protocol.SymbolKindFunction),
					ContainerName: "server",
					URI:           uri.File("/workspace/server.go").String(),
					Range:         &NavigationRange{StartLine: 10, StartColumn: 2, EndLine: 10, EndColumn: 9},
				},
			},
		},
		{
			name: "workspace symbol location",
			result: protocol.WorkspaceSymbolSlice{
				{
					Name: "pkg",
					Kind: protocol.SymbolKindPackage,
					Location: &protocol.Location{
						URI: uri.File("/workspace/pkg"),
						Range: protocol.Range{
							Start: protocol.Position{Line: 3, Character: 0},
							End:   protocol.Position{Line: 3, Character: 7},
						},
					},
				},
			},
			want: []WorkspaceSymbol{
				{
					Name: "pkg",
					Kind: fmt.Sprint(protocol.SymbolKindPackage),
					URI:  uri.File("/workspace/pkg").String(),
					Range: &NavigationRange{
						StartLine:   3,
						StartColumn: 0,
						EndLine:     3,
						EndColumn:   7,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			srv := &featureServer{
				capabilities: protocol.ServerCapabilities{WorkspaceSymbolProvider: &protocol.WorkspaceSymbolOptions{}},
				symbolResult: tt.result,
			}
			mgr := newFeatureManager(t, srv, root)

			got, err := mgr.WorkspaceSymbols().Lookup(t.Context(), "go", "handler")
			if err != nil {
				t.Fatalf("WorkspaceSymbols.Lookup: %v", err)
			}
			if diff := gocmp.Diff(tt.want, got); diff != "" {
				t.Fatalf("workspace symbols mismatch (-want +got):\n%s", diff)
			}

			calls := srv.symbolCalls()
			if len(calls) != 1 {
				t.Fatalf("symbol calls = %d, want 1", len(calls))
			}
			if calls[0].Query != "handler" {
				t.Fatalf("symbol query = %q, want handler", calls[0].Query)
			}
		})
	}
}

func TestFlattenWorkspaceSymbolsSupportsURIOnlyLocations(t *testing.T) {
	t.Parallel()

	got := flattenWorkspaceSymbols(protocol.WorkspaceSymbolSlice{
		{
			Name:     "pkg",
			Kind:     protocol.SymbolKindPackage,
			Location: &protocol.LocationUriOnly{URI: uri.File("/workspace/pkg")},
		},
	})
	want := []WorkspaceSymbol{
		{
			Name: "pkg",
			Kind: fmt.Sprint(protocol.SymbolKindPackage),
			URI:  uri.File("/workspace/pkg").String(),
		},
	}
	if diff := gocmp.Diff(want, got); diff != "" {
		t.Fatalf("workspace symbols mismatch (-want +got):\n%s", diff)
	}
}

func TestFormattingRangeFormattingAndRenameReturnWorkspaceEditPreviews(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	fileURI := uri.File(path)
	formatRange := protocol.Range{Start: protocol.Position{Line: 0, Character: 0}, End: protocol.Position{Line: 0, Character: 0}}
	rangeFormatRange := protocol.Range{Start: protocol.Position{Line: 1, Character: 0}, End: protocol.Position{Line: 1, Character: 7}}
	renameRange := protocol.Range{Start: protocol.Position{Line: 2, Character: 4}, End: protocol.Position{Line: 2, Character: 8}}
	srv := &featureServer{
		capabilities: protocol.ServerCapabilities{
			DocumentFormattingProvider:      protocol.Boolean(true),
			DocumentRangeFormattingProvider: &protocol.DocumentRangeFormattingOptions{},
			RenameProvider:                  &protocol.RenameOptions{},
		},
		formattingEdits: []protocol.TextEdit{
			{Range: formatRange, NewText: "// formatted\n"},
		},
		rangeFormattingEdits: []protocol.TextEdit{
			{Range: rangeFormatRange, NewText: "renamed"},
		},
		renameEdit: &protocol.WorkspaceEdit{
			Changes: map[uri.URI][]protocol.TextEdit{
				fileURI: {
					{Range: renameRange, NewText: "Renamed"},
				},
			},
		},
	}
	mgr := newFeatureManager(t, srv, root)

	formatOptions := protocol.FormattingOptions{TabSize: 8, InsertSpaces: false}
	gotFormat, err := mgr.Formatting().Format(t.Context(), "go", path, "package main\n", formatOptions)
	if err != nil {
		t.Fatalf("Formatting.Format: %v", err)
	}
	wantFormat := WorkspaceEdit{Changes: map[string][]WorkspaceTextEdit{
		fileURI.String(): {{Range: NavigationRange{StartLine: 0, StartColumn: 0, EndLine: 0, EndColumn: 0}, NewText: "// formatted\n"}},
	}}
	if diff := gocmp.Diff(wantFormat, gotFormat); diff != "" {
		t.Fatalf("format workspace edit mismatch (-want +got):\n%s", diff)
	}

	gotRangeFormat, err := mgr.Formatting().RangeFormat(t.Context(), "go", path, "package main\n", rangeFormatRange, formatOptions)
	if err != nil {
		t.Fatalf("Formatting.RangeFormat: %v", err)
	}
	wantRangeFormat := WorkspaceEdit{Changes: map[string][]WorkspaceTextEdit{
		fileURI.String(): {{Range: NavigationRange{StartLine: 1, StartColumn: 0, EndLine: 1, EndColumn: 7}, NewText: "renamed"}},
	}}
	if diff := gocmp.Diff(wantRangeFormat, gotRangeFormat); diff != "" {
		t.Fatalf("range format workspace edit mismatch (-want +got):\n%s", diff)
	}

	renamePos := protocol.Position{Line: 2, Character: 4}
	gotRename, err := mgr.Rename().Preview(t.Context(), "go", path, "package main\n", renamePos, "Renamed")
	if err != nil {
		t.Fatalf("Rename.Preview: %v", err)
	}
	wantRename := WorkspaceEdit{Changes: map[string][]WorkspaceTextEdit{
		fileURI.String(): {{Range: NavigationRange{StartLine: 2, StartColumn: 4, EndLine: 2, EndColumn: 8}, NewText: "Renamed"}},
	}, DocumentChanges: []WorkspaceDocumentChange{}}
	if diff := gocmp.Diff(wantRename, gotRename); diff != "" {
		t.Fatalf("rename workspace edit mismatch (-want +got):\n%s", diff)
	}

	formatCalls := srv.formattingCalls()
	if len(formatCalls) != 1 {
		t.Fatalf("format calls = %d, want 1", len(formatCalls))
	}
	if formatCalls[0].TextDocument.URI != fileURI {
		t.Fatalf("format URI = %q, want %q", formatCalls[0].TextDocument.URI, fileURI)
	}
	if diff := gocmp.Diff(formatOptions, formatCalls[0].Options); diff != "" {
		t.Fatalf("format options mismatch (-want +got):\n%s", diff)
	}
	rangeCalls := srv.rangeFormattingCalls()
	if len(rangeCalls) != 1 {
		t.Fatalf("range format calls = %d, want 1", len(rangeCalls))
	}
	if rangeCalls[0].Range != rangeFormatRange {
		t.Fatalf("range format range = %+v, want %+v", rangeCalls[0].Range, rangeFormatRange)
	}
	renameCalls := srv.renameCalls()
	if len(renameCalls) != 1 {
		t.Fatalf("rename calls = %d, want 1", len(renameCalls))
	}
	if renameCalls[0].Position != renamePos {
		t.Fatalf("rename position = %+v, want %+v", renameCalls[0].Position, renamePos)
	}
	if renameCalls[0].NewName != "Renamed" {
		t.Fatalf("rename newName = %q, want Renamed", renameCalls[0].NewName)
	}
}

func TestCodeActionsAndCodeLensesResolveWhenSupported(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	fileURI := uri.File(path)
	actionKind := protocol.CodeActionKindQuickFix
	isPreferred := true
	tooltip := "run server command"
	actionRange := protocol.Range{Start: protocol.Position{Line: 0, Character: 0}, End: protocol.Position{Line: 0, Character: 4}}
	lensRange := protocol.Range{Start: protocol.Position{Line: 4, Character: 0}, End: protocol.Position{Line: 4, Character: 0}}
	resolveProvider := true
	srv := &featureServer{
		capabilities: protocol.ServerCapabilities{
			CodeActionProvider: &protocol.CodeActionOptions{ResolveProvider: &resolveProvider},
			CodeLensProvider:   &protocol.CodeLensOptions{ResolveProvider: &resolveProvider},
		},
		codeActions: []protocol.CommandOrCodeAction{
			&protocol.Command{Title: "Run", Tooltip: &tooltip, Command: "server.run"},
			&protocol.CodeAction{
				Title:       "Fix",
				Kind:        &actionKind,
				IsPreferred: &isPreferred,
				Data:        protocol.LSPAny(`{"id":1}`),
			},
		},
		codeActionResolveResult: &protocol.CodeAction{
			Title:       "Fix",
			Kind:        &actionKind,
			IsPreferred: &isPreferred,
			Edit: &protocol.WorkspaceEdit{
				Changes: map[uri.URI][]protocol.TextEdit{
					fileURI: {{Range: actionRange, NewText: "fixed"}},
				},
			},
			Command: protocol.Command{Title: "Apply", Command: "server.apply"},
		},
		codeLenses: []protocol.CodeLens{
			{Range: lensRange, Data: protocol.LSPAny(`{"lens":1}`)},
		},
		codeLensResolveResult: &protocol.CodeLens{
			Range:   lensRange,
			Command: protocol.Command{Title: "Test", Command: "go.test"},
		},
	}
	mgr := newFeatureManager(t, srv, root)

	gotActions, err := mgr.CodeActions().Lookup(t.Context(), "go", path, "package main\n", actionRange, []protocol.CodeActionKind{actionKind}, true)
	if err != nil {
		t.Fatalf("CodeActions.Lookup: %v", err)
	}
	wantEdit := WorkspaceEdit{Changes: map[string][]WorkspaceTextEdit{
		fileURI.String(): {{Range: NavigationRange{StartLine: 0, StartColumn: 0, EndLine: 0, EndColumn: 4}, NewText: "fixed"}},
	}, DocumentChanges: []WorkspaceDocumentChange{}}
	wantActions := []CodeAction{
		{Title: "Run", Command: &Command{Title: "Run", Tooltip: tooltip, Command: "server.run"}},
		{Title: "Fix", Kind: string(actionKind), IsPreferred: &isPreferred, Edit: &wantEdit, Command: &Command{Title: "Apply", Command: "server.apply"}},
	}
	if diff := gocmp.Diff(wantActions, gotActions); diff != "" {
		t.Fatalf("code actions mismatch (-want +got):\n%s", diff)
	}
	actionCalls := srv.codeActionCalls()
	if len(actionCalls) != 1 {
		t.Fatalf("code action calls = %d, want 1", len(actionCalls))
	}
	if actionCalls[0].TextDocument.URI != fileURI {
		t.Fatalf("code action URI = %q, want %q", actionCalls[0].TextDocument.URI, fileURI)
	}
	if actionCalls[0].Range != actionRange {
		t.Fatalf("code action range = %+v, want %+v", actionCalls[0].Range, actionRange)
	}
	if diff := gocmp.Diff([]protocol.CodeActionKind{actionKind}, actionCalls[0].Context.Only); diff != "" {
		t.Fatalf("code action only mismatch (-want +got):\n%s", diff)
	}
	if got := len(srv.codeActionResolveCalls()); got != 1 {
		t.Fatalf("codeAction/resolve calls = %d, want 1", got)
	}

	gotLenses, err := mgr.CodeLenses().Lookup(t.Context(), "go", path, "package main\n", true)
	if err != nil {
		t.Fatalf("CodeLenses.Lookup: %v", err)
	}
	wantLenses := []CodeLens{
		{
			Range:   NavigationRange{StartLine: 4, StartColumn: 0, EndLine: 4, EndColumn: 0},
			Command: &Command{Title: "Test", Command: "go.test"},
		},
	}
	if diff := gocmp.Diff(wantLenses, gotLenses); diff != "" {
		t.Fatalf("code lenses mismatch (-want +got):\n%s", diff)
	}
	lensCalls := srv.codeLensCalls()
	if len(lensCalls) != 1 {
		t.Fatalf("code lens calls = %d, want 1", len(lensCalls))
	}
	if lensCalls[0].TextDocument.URI != fileURI {
		t.Fatalf("code lens URI = %q, want %q", lensCalls[0].TextDocument.URI, fileURI)
	}
	if got := len(srv.codeLensResolveCalls()); got != 1 {
		t.Fatalf("codeLens/resolve calls = %d, want 1", got)
	}
}

func TestCommandsExecuteUsesAdvertisedCommandAndRawJSONArguments(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	srv := &featureServer{
		capabilities: protocol.ServerCapabilities{
			ExecuteCommandProvider: protocol.ExecuteCommandOptions{Commands: []string{"server.test"}},
		},
		executeResult: protocol.LSPAny(`{"ok":true}`),
	}
	mgr := newFeatureManager(t, srv, root)
	args := []protocol.LSPAny{protocol.LSPAny(`"arg"`), protocol.LSPAny(`1`)}

	got, err := mgr.Commands().Execute(t.Context(), "go", "server.test", args, false)
	if err != nil {
		t.Fatalf("Commands.Execute: %v", err)
	}
	if string(got) != `{"ok":true}` {
		t.Fatalf("execute result = %s, want {\"ok\":true}", got)
	}

	calls := srv.executeCalls()
	if len(calls) != 1 {
		t.Fatalf("execute calls = %d, want 1", len(calls))
	}
	if calls[0].Command != "server.test" {
		t.Fatalf("execute command = %q, want server.test", calls[0].Command)
	}
	if len(calls[0].Arguments) != 2 {
		t.Fatalf("execute argument count = %d, want 2", len(calls[0].Arguments))
	}
	if string(calls[0].Arguments[0]) != `"arg"` || string(calls[0].Arguments[1]) != `1` {
		t.Fatalf("execute arguments = [%s %s], want [\"arg\" 1]", calls[0].Arguments[0], calls[0].Arguments[1])
	}
}
