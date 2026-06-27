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
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// fakeServer is an in-memory [protocol.Server] test double. It records the
// requests mcp-lsp issues and answers diagnostic requests with a configurable report. It
// can also push publishDiagnostics to the client through the dispatcher handed
// back by [protocol.NewServer].
type fakeServer struct {
	protocol.UnimplementedServer

	mu          sync.Mutex
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

	definitionResult   protocol.DefinitionResult
	definitionErr      error
	definitionRequests []protocol.DefinitionParams

	client protocol.Client
}

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

func (f *fakeServer) Initialize(_ context.Context, _ *protocol.InitializeParams) (*protocol.InitializeResult, error) {
	res := &protocol.InitializeResult{
		ServerInfo: protocol.ServerInfo{Name: "fake"},
	}
	if f.pullSupported {
		res.Capabilities.DiagnosticProvider = &protocol.DiagnosticOptions{
			InterFileDependencies: true,
		}
	}
	if f.implementationSupported {
		res.Capabilities.ImplementationProvider = protocol.Boolean(true)
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

// fakeManager wraps a wired session so [Diagnostics.Lookup] can drive it without
// a real [Manager.session] spawn.
func fakeDiagnostics(sess *serverSession, lang string) *Diagnostics {
	mgr := &Manager{
		cfg:      map[string]ServerConfig{lang: {LanguageID: protocol.LanguageKindGo}},
		sessions: map[string]*serverSession{lang: sess},
		logger:   slog.New(slog.DiscardHandler),
	}
	return &Diagnostics{mgr: mgr, settle: 50 * time.Millisecond, timeout: 2 * time.Second}
}

func TestDiagnosticsLookupPull(t *testing.T) {
	t.Parallel()

	fake := &fakeServer{
		pullSupported: true,
		pullReport: &protocol.RelatedFullDocumentDiagnosticReport{
			Kind: string(protocol.DocumentDiagnosticReportKindFull),
			Items: []protocol.Diagnostic{
				{
					Range: protocol.Range{
						Start: protocol.Position{Line: 0, Character: 0},
						End:   protocol.Position{Line: 0, Character: 3},
					},
					Severity: protocol.DiagnosticSeverityError,
					Source:   protocol.NewOptional("compiler"),
					Code:     protocol.String("E001"),
					Message:  protocol.String("undeclared name: foo"),
				},
			},
		},
	}
	sess := wireSession(t, fake)
	if !sess.pullSupported {
		t.Fatal("session did not detect pull support advertised by the fake")
	}

	diags := fakeDiagnostics(sess, "go")
	got, err := diags.Lookup(t.Context(), "go", "/workspace/main.go", "package main\n")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	want := []Diagnostic{
		{
			StartLine: 0, StartColumn: 0, EndLine: 0, EndColumn: 3,
			Severity: "error", Source: "compiler", Code: "E001",
			Message: "undeclared name: foo",
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Lookup diagnostics mismatch (-want +got):\n%s", diff)
	}

	// didOpen must carry the caller's text and the configured language.
	opened := fake.openedDocs()
	if len(opened) != 1 {
		t.Fatalf("expected exactly one didOpen, got %d", len(opened))
	}
	if opened[0].TextDocument.Text != "package main\n" {
		t.Errorf("didOpen text = %q, want %q", opened[0].TextDocument.Text, "package main\n")
	}
	if opened[0].TextDocument.LanguageID != protocol.LanguageKindGo {
		t.Errorf("didOpen languageID = %q, want %q", opened[0].TextDocument.LanguageID, protocol.LanguageKindGo)
	}
}

func TestDiagnosticsLookupPush(t *testing.T) {
	t.Parallel()

	fake := &fakeServer{pullSupported: false}
	sess := wireSession(t, fake)
	if sess.pullSupported {
		t.Fatal("session advertised pull support the fake did not offer")
	}
	clock := newFakeClock()
	sess.store.nowFn = clock.Now

	u := uri.File("/workspace/main.go")
	diags := fakeDiagnostics(sess, "go")

	// The server pushes an empty pre-analysis report, then the real diagnostics,
	// both within the settle window after didOpen. waitSettled must return the
	// latter.
	fake.onDidOpen = func(_ context.Context, _ *protocol.DidOpenTextDocumentParams) error {
		sess.store.publish(&protocol.PublishDiagnosticsParams{URI: u})
		sess.store.publish(&protocol.PublishDiagnosticsParams{
			URI: u,
			Diagnostics: []protocol.Diagnostic{
				{
					Range: protocol.Range{
						Start: protocol.Position{Line: 4, Character: 1},
						End:   protocol.Position{Line: 4, Character: 6},
					},
					Severity: protocol.DiagnosticSeverityWarning,
					Message:  protocol.String("unused variable"),
				},
			},
		})
		clock.Advance(diags.settle)
		sess.store.broadcastAll()
		return nil
	}

	got, err := diags.Lookup(t.Context(), "go", "/workspace/main.go", "package main\n")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	want := []Diagnostic{
		{
			StartLine: 4, StartColumn: 1, EndLine: 4, EndColumn: 6,
			Severity: "warning", Message: "unused variable",
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Lookup diagnostics mismatch (-want +got):\n%s", diff)
	}
}

func TestPublishDiagnosticsNotificationReachesStore(t *testing.T) {
	t.Parallel()

	fake := &fakeServer{pullSupported: false}
	sess := wireSession(t, fake)
	u := uri.File("/workspace/main.go")

	if err := fake.client.PublishDiagnostics(t.Context(), &protocol.PublishDiagnosticsParams{
		URI: u,
		Diagnostics: []protocol.Diagnostic{
			{
				Range: protocol.Range{
					Start: protocol.Position{Line: 2, Character: 3},
					End:   protocol.Position{Line: 2, Character: 8},
				},
				Severity: protocol.DiagnosticSeverityError,
				Message:  protocol.String("wired diagnostic"),
			},
		},
	}); err != nil {
		t.Fatalf("PublishDiagnostics: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	raw, err := sess.store.waitSettledAfter(ctx, u, 0, 0)
	if err != nil {
		t.Fatalf("wait for published diagnostics: %v", err)
	}

	want := []Diagnostic{
		{
			StartLine: 2, StartColumn: 3, EndLine: 2, EndColumn: 8,
			Severity: "error", Message: "wired diagnostic",
		},
	}
	if diff := cmp.Diff(want, flattenDiagnostics(raw)); diff != "" {
		t.Errorf("published diagnostics mismatch (-want +got):\n%s", diff)
	}
}

func TestDiagnosticsLookupPushIgnoresCachedBaseline(t *testing.T) {
	t.Parallel()

	fake := &fakeServer{pullSupported: false}
	sess := wireSession(t, fake)
	if sess.pullSupported {
		t.Fatal("session advertised pull support the fake did not offer")
	}
	clock := newFakeClock()
	sess.store.nowFn = clock.Now

	u := uri.File("/workspace/main.go")
	sess.store.publish(&protocol.PublishDiagnosticsParams{
		URI: u,
		Diagnostics: []protocol.Diagnostic{
			{
				Range: protocol.Range{
					Start: protocol.Position{Line: 1, Character: 0},
					End:   protocol.Position{Line: 1, Character: 6},
				},
				Severity: protocol.DiagnosticSeverityError,
				Message:  protocol.String("stale cached diagnostic"),
			},
		},
	})

	diags := fakeDiagnostics(sess, "go")
	clock.Advance(diags.settle + time.Millisecond)

	published := make(chan struct{})
	fake.onDidOpen = func(_ context.Context, _ *protocol.DidOpenTextDocumentParams) error {
		go func() {
			time.Sleep(10 * time.Millisecond)
			sess.store.publish(&protocol.PublishDiagnosticsParams{
				URI: u,
				Diagnostics: []protocol.Diagnostic{
					{
						Range: protocol.Range{
							Start: protocol.Position{Line: 4, Character: 1},
							End:   protocol.Position{Line: 4, Character: 6},
						},
						Severity: protocol.DiagnosticSeverityWarning,
						Message:  protocol.String("fresh diagnostic"),
					},
				},
			})
			clock.Advance(diags.settle)
			sess.store.broadcastAll()
			close(published)
		}()
		return nil
	}

	got, err := diags.Lookup(t.Context(), "go", "/workspace/main.go", "package main\n")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}

	select {
	case <-published:
	case <-time.After(2 * time.Second):
		t.Fatal("fresh diagnostics were not published")
	}

	want := []Diagnostic{
		{
			StartLine: 4, StartColumn: 1, EndLine: 4, EndColumn: 6,
			Severity: "warning", Message: "fresh diagnostic",
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Lookup diagnostics mismatch (-want +got):\n%s", diff)
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

func int32Ptr(v int32) *int32 {
	return &v
}
