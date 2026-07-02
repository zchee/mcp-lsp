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
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// fakeClock is a deterministic clock for integration-style tests that cannot
// run inside a testing/synctest bubble because they use jsonrpc2 over pipes.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Unix(0, 0)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
}

// fakeServer is an in-memory [protocol.Server] test double. It records the
// requests mcp-lsp issues, advertises configurable capabilities, returns
// configured method results, and can push publishDiagnostics to the client
// through the dispatcher handed back by [protocol.NewServer].
type fakeServer struct {
	protocol.UnimplementedServer

	mu           sync.Mutex
	capabilities protocol.ServerCapabilities

	opened      []protocol.DidOpenTextDocumentParams
	onDidOpen   func(context.Context, *protocol.DidOpenTextDocumentParams) error
	changed     []protocol.DidChangeTextDocumentParams
	onDidChange func(context.Context, *protocol.DidChangeTextDocumentParams) error
	closed      []protocol.DidCloseTextDocumentParams
	onDidClose  func(context.Context, *protocol.DidCloseTextDocumentParams) error

	pullSupported bool
	pullReport    protocol.DocumentDiagnosticReport

	implementationSupported bool
	implementationResult    protocol.DefinitionResult
	implementationErr       error
	implementationRequests  []protocol.ImplementationParams

	referencesSupported bool
	referencesResult    []protocol.Location
	referencesErr       error
	referencesRequests  []protocol.ReferenceParams

	declarationSupported bool
	declarationResult    protocol.DeclarationResult
	declarationErr       error
	declarationRequests  []protocol.DeclarationParams

	typeDefinitionSupported bool
	typeDefinitionResult    protocol.DefinitionResult
	typeDefinitionErr       error
	typeDefinitionRequests  []protocol.TypeDefinitionParams

	documentSymbolSupported bool
	documentSymbolResult    protocol.DocumentSymbolResult
	documentSymbolErr       error
	documentSymbolRequests  []protocol.DocumentSymbolParams

	callHierarchySupported       bool
	callHierarchyItems           []protocol.CallHierarchyItem
	callHierarchyErr             error
	callHierarchyPrepareRequests []protocol.CallHierarchyPrepareParams
	incomingCalls                []protocol.CallHierarchyIncomingCall
	incomingCallsErr             error
	incomingCallsRequests        []protocol.CallHierarchyIncomingCallsParams
	outgoingCalls                []protocol.CallHierarchyOutgoingCall
	outgoingCallsErr             error
	outgoingCallsRequests        []protocol.CallHierarchyOutgoingCallsParams

	typeHierarchySupported       bool
	typeHierarchyItems           []protocol.TypeHierarchyItem
	typeHierarchyErr             error
	typeHierarchyPrepareRequests []protocol.TypeHierarchyPrepareParams
	supertypes                   []protocol.TypeHierarchyItem
	supertypesErr                error
	supertypesRequests           []protocol.TypeHierarchySupertypesParams
	subtypes                     []protocol.TypeHierarchyItem
	subtypesErr                  error
	subtypesRequests             []protocol.TypeHierarchySubtypesParams

	definitionResult   protocol.DefinitionResult
	definitionErr      error
	definitionRequests []protocol.DefinitionParams

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

	client protocol.Client
}

var _ protocol.Server = (*fakeServer)(nil)

func (f *fakeServer) Initialize(_ context.Context, _ *protocol.InitializeParams) (*protocol.InitializeResult, error) {
	f.mu.Lock()
	capabilities := f.capabilities
	pullSupported := f.pullSupported
	implementationSupported := f.implementationSupported
	referencesSupported := f.referencesSupported
	declarationSupported := f.declarationSupported
	typeDefinitionSupported := f.typeDefinitionSupported
	documentSymbolSupported := f.documentSymbolSupported
	callHierarchySupported := f.callHierarchySupported
	typeHierarchySupported := f.typeHierarchySupported
	f.mu.Unlock()

	res := &protocol.InitializeResult{
		ServerInfo:   protocol.ServerInfo{Name: "fake"},
		Capabilities: capabilities,
	}
	if pullSupported {
		res.Capabilities.DiagnosticProvider = &protocol.DiagnosticOptions{
			InterFileDependencies: true,
		}
	}
	if implementationSupported {
		res.Capabilities.ImplementationProvider = protocol.Boolean(true)
	}
	if referencesSupported {
		res.Capabilities.ReferencesProvider = protocol.Boolean(true)
	}
	if declarationSupported {
		res.Capabilities.DeclarationProvider = protocol.Boolean(true)
	}
	if typeDefinitionSupported {
		res.Capabilities.TypeDefinitionProvider = protocol.Boolean(true)
	}
	if documentSymbolSupported {
		res.Capabilities.DocumentSymbolProvider = protocol.Boolean(true)
	}
	if callHierarchySupported {
		res.Capabilities.CallHierarchyProvider = protocol.Boolean(true)
	}
	if typeHierarchySupported {
		res.Capabilities.TypeHierarchyProvider = protocol.Boolean(true)
	}
	return res, nil
}

func (f *fakeServer) DidOpen(ctx context.Context, params *protocol.DidOpenTextDocumentParams) error {
	f.mu.Lock()
	f.opened = append(f.opened, *params)
	onDidOpen := f.onDidOpen
	f.mu.Unlock()

	if onDidOpen != nil {
		return onDidOpen(ctx, params)
	}
	return nil
}

func (f *fakeServer) DidChange(ctx context.Context, params *protocol.DidChangeTextDocumentParams) error {
	f.mu.Lock()
	f.changed = append(f.changed, *params)
	onDidChange := f.onDidChange
	f.mu.Unlock()

	if onDidChange != nil {
		return onDidChange(ctx, params)
	}
	return nil
}

func (f *fakeServer) DidClose(ctx context.Context, params *protocol.DidCloseTextDocumentParams) error {
	f.mu.Lock()
	f.closed = append(f.closed, *params)
	onDidClose := f.onDidClose
	f.mu.Unlock()

	if onDidClose != nil {
		return onDidClose(ctx, params)
	}
	return nil
}

func (f *fakeServer) Diagnostic(_ context.Context, _ *protocol.DocumentDiagnosticParams) (protocol.DocumentDiagnosticReport, error) {
	return f.pullReport, nil
}

func (f *fakeServer) openedDocs() []protocol.DidOpenTextDocumentParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.DidOpenTextDocumentParams(nil), f.opened...)
}

func (f *fakeServer) changedDocs() []protocol.DidChangeTextDocumentParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.DidChangeTextDocumentParams(nil), f.changed...)
}

func (f *fakeServer) closedDocs() []protocol.DidCloseTextDocumentParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.DidCloseTextDocumentParams(nil), f.closed...)
}

// wireSessionCore connects srv to a ready serverSession over an in-memory pipe
// and returns the session together with the server-side dispatcher used to push
// notifications back to the client. pullSupported reflects the capabilities srv
// advertised in its initialize response. Cleanup of both connections and their
// contexts is registered on t.
func wireSessionCore(t *testing.T, srv protocol.Server) (*serverSession, protocol.Client) {
	t.Helper()

	clientEnd, serverEnd := net.Pipe()

	serverCtx, serverCancel := context.WithCancel(context.Background())
	_, serverConn, client := protocol.NewServer(serverCtx, srv, jsonrpc2.NewHeaderStream(serverEnd))

	st := newStore()
	logger := slog.New(slog.DiscardHandler)
	clientCtx, clientCancel := context.WithCancel(context.Background())
	lspClient := newClient(st, logger)
	_, clientConn, server := protocol.NewClient(clientCtx, lspClient, jsonrpc2.NewHeaderStream(clientEnd))

	res, err := server.Initialize(clientCtx, initializeParams(uri.File(t.TempDir())))
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := server.Initialized(clientCtx, &protocol.InitializedParams{}); err != nil {
		t.Fatalf("initialized: %v", err)
	}

	sess := &serverSession{
		ready:                   make(chan struct{}),
		capabilities:            snapshotCapabilities(&res.Capabilities),
		pullSupported:           res.Capabilities.DiagnosticProvider != nil,
		implementationSupported: implementationProviderSupported(res.Capabilities.ImplementationProvider),
		conn:                    clientConn,
		server:                  server,
		client:                  lspClient,
		store:                   st,
		logger:                  logger,
		cancel:                  clientCancel,
	}
	// Consume the [sync.Once] so [Manager.session] does not attempt a real spawn
	// for this pre-wired session, and signal readiness.
	sess.once.Do(func() {})
	close(sess.ready)

	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
		clientCancel()
		serverCancel()
	})
	return sess, client
}

// wireSession connects fake to a ready serverSession over an in-memory pipe and
// returns the session. The fake's pull capability is reflected on the session,
// and the server-side dispatcher is wired into fake.client so the fake can push
// publishDiagnostics back to the client.
func wireSession(t *testing.T, fake *fakeServer) *serverSession {
	t.Helper()

	sess, client := wireSessionCore(t, fake)
	fake.client = client
	return sess
}

func (f *fakeServer) Hover(_ context.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.hoverRequests = append(f.hoverRequests, *params)
	return f.hoverResult, nil
}

func (f *fakeServer) Symbols(_ context.Context, params *protocol.WorkspaceSymbolParams) (protocol.WorkspaceSymbolResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.symbolRequests = append(f.symbolRequests, *params)
	return f.symbolResult, nil
}

func (f *fakeServer) Formatting(_ context.Context, params *protocol.DocumentFormattingParams) ([]protocol.TextEdit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.formattingRequests = append(f.formattingRequests, *params)
	return slices.Clone(f.formattingEdits), nil
}

func (f *fakeServer) RangeFormatting(_ context.Context, params *protocol.DocumentRangeFormattingParams) ([]protocol.TextEdit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.rangeFormattingRequests = append(f.rangeFormattingRequests, *params)
	return slices.Clone(f.rangeFormattingEdits), nil
}

func (f *fakeServer) Rename(_ context.Context, params *protocol.RenameParams) (*protocol.WorkspaceEdit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.renameRequests = append(f.renameRequests, *params)
	return f.renameEdit, nil
}

func (f *fakeServer) CodeAction(_ context.Context, params *protocol.CodeActionParams) ([]protocol.CommandOrCodeAction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.codeActionRequests = append(f.codeActionRequests, *params)
	return slices.Clone(f.codeActions), nil
}

func (f *fakeServer) CodeActionResolve(_ context.Context, params *protocol.CodeAction) (*protocol.CodeAction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.codeActionResolveRequests = append(f.codeActionResolveRequests, *params)
	return f.codeActionResolveResult, nil
}

func (f *fakeServer) CodeLens(_ context.Context, params *protocol.CodeLensParams) ([]protocol.CodeLens, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.codeLensRequests = append(f.codeLensRequests, *params)
	return slices.Clone(f.codeLenses), nil
}

func (f *fakeServer) CodeLensResolve(_ context.Context, params *protocol.CodeLens) (*protocol.CodeLens, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.codeLensResolveRequests = append(f.codeLensResolveRequests, *params)
	return f.codeLensResolveResult, nil
}

func (f *fakeServer) ExecuteCommand(_ context.Context, params *protocol.ExecuteCommandParams) (protocol.LSPAny, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.executeRequests = append(f.executeRequests, *params)
	return f.executeResult, nil
}

func (f *fakeServer) hoverCalls() []protocol.HoverParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.hoverRequests)
}

func (f *fakeServer) symbolCalls() []protocol.WorkspaceSymbolParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.symbolRequests)
}

func (f *fakeServer) formattingCalls() []protocol.DocumentFormattingParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.formattingRequests)
}

func (f *fakeServer) rangeFormattingCalls() []protocol.DocumentRangeFormattingParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.rangeFormattingRequests)
}

func (f *fakeServer) renameCalls() []protocol.RenameParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.renameRequests)
}

func (f *fakeServer) codeActionCalls() []protocol.CodeActionParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.codeActionRequests)
}

func (f *fakeServer) codeActionResolveCalls() []protocol.CodeAction {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.codeActionResolveRequests)
}

func (f *fakeServer) codeLensCalls() []protocol.CodeLensParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.codeLensRequests)
}

func (f *fakeServer) codeLensResolveCalls() []protocol.CodeLens {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.codeLensResolveRequests)
}

func (f *fakeServer) executeCalls() []protocol.ExecuteCommandParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.executeRequests)
}

func newFakeServerManager(t *testing.T, srv *fakeServer, rootDir string) *Manager {
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

func requireNoDocumentSync(t *testing.T, srv *fakeServer) {
	t.Helper()

	if got := len(srv.openedDocs()); got != 0 {
		t.Fatalf("unsupported calls opened %d documents, want 0", got)
	}
	if got := len(srv.changedDocs()); got != 0 {
		t.Fatalf("unsupported calls changed %d documents, want 0", got)
	}
}

func TestSessionShutdownIsClean(t *testing.T) {
	t.Parallel()

	fake := &fakeServer{pullSupported: true}
	sess := wireSession(t, fake)

	// Start the connection watchdog as start would, and observe its return.
	watchDone := make(chan struct{})
	go func() {
		sess.watch()
		close(watchDone)
	}()

	if err := sess.shutdown(t.Context()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	// The connection read loop must have exited.
	select {
	case <-sess.conn.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("conn.Done() was not closed after shutdown")
	}

	// The watchdog goroutine must have returned (no goroutine leak).
	select {
	case <-watchDone:
	case <-time.After(2 * time.Second):
		t.Fatal("watch goroutine did not return after shutdown")
	}

	if !sess.dead.Load() {
		t.Error("session was not marked dead after the connection closed")
	}

	// shutdown is idempotent: a second call must not error or block.
	if err := sess.shutdown(t.Context()); err != nil {
		t.Errorf("second shutdown: %v", err)
	}
}

func TestFakeServerTracksDocumentLifecycle(t *testing.T) {
	t.Parallel()

	fake := &fakeServer{}
	change := protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{
				URI: uri.File("/workspace/main.go"),
			},
			Version: 2,
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{
			&protocol.TextDocumentContentChangeWholeDocument{
				Text: "package main\n",
			},
		},
	}
	closeParams := protocol.DidCloseTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: uri.File("/workspace/main.go"),
		},
	}

	if err := fake.DidChange(t.Context(), &change); err != nil {
		t.Fatalf("DidChange: %v", err)
	}
	if err := fake.DidClose(t.Context(), &closeParams); err != nil {
		t.Fatalf("DidClose: %v", err)
	}

	changed := fake.changedDocs()
	if len(changed) != 1 {
		t.Fatalf("changed docs = %d, want 1", len(changed))
	}
	if changed[0].TextDocument.URI != uri.File("/workspace/main.go") {
		t.Errorf("changed doc URI = %q, want %q", changed[0].TextDocument.URI, uri.File("/workspace/main.go"))
	}
	if changed[0].TextDocument.Version != 2 {
		t.Errorf("changed doc version = %d, want %d", changed[0].TextDocument.Version, 2)
	}

	closed := fake.closedDocs()
	if len(closed) != 1 {
		t.Fatalf("closed docs = %d, want 1", len(closed))
	}
	if closed[0].TextDocument.URI != uri.File("/workspace/main.go") {
		t.Errorf("closed doc URI = %q, want %q", closed[0].TextDocument.URI, uri.File("/workspace/main.go"))
	}
}
