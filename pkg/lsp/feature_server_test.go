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
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"

	"go.lsp.dev/protocol"
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

func requireNoFeatureSync(t *testing.T, srv *featureServer) {
	t.Helper()

	if got := len(srv.openedDocs()); got != 0 {
		t.Fatalf("unsupported feature calls opened %d documents, want 0", got)
	}
	if got := len(srv.changedDocs()); got != 0 {
		t.Fatalf("unsupported feature calls changed %d documents, want 0", got)
	}
}
