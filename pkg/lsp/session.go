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
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// shutdownWait bounds how long shutdown waits for a server process to exit
// cleanly before killing it.
const shutdownWait = 5 * time.Second

// serverSession is one language server subprocess together with its jsonrpc2
// connection and lifecycle state. Its [sync.Once] guards a single start; because
// a fired [sync.Once] cannot be reused, a dead session is replaced wholesale by
// the [Manager] rather than restarted in place.
type serverSession struct {
	once                    sync.Once
	ready                   chan struct{}
	initErr                 error
	capabilities            sessionCapabilities
	pullSupported           bool
	implementationSupported bool
	docsMu                  sync.Mutex
	openDocs                map[uri.URI]*openedDocument

	cmd    *exec.Cmd
	conn   jsonrpc2.Conn
	server protocol.Server
	client *Client
	store  *store
	logger *slog.Logger
	cancel context.CancelFunc

	dead         atomic.Bool
	shutdownOnce sync.Once
	shutdownErr  error

	// startFn performs the one-time spawn-and-initialize. It defaults to the
	// real start method and is a seam for tests that exercise the [Manager]'s
	// session lifecycle without spawning a subprocess.
	startFn func(ctx context.Context, cfg ServerConfig, rootURI uri.URI)
}

// newSession returns an unstarted session bound to the diagnostics cache store.
func newSession(store *store, logger *slog.Logger) *serverSession {
	s := &serverSession{
		ready:  make(chan struct{}),
		store:  store,
		logger: logger,
	}
	s.startFn = s.start
	return s
}

type sessionCapabilities struct {
	pullDiagnostics   bool
	implementation    bool
	hover             bool
	codeAction        bool
	codeActionResolve bool
	codeLens          bool
	codeLensResolve   bool
	workspaceSymbol   bool
	formatting        bool
	rangeFormatting   bool
	rename            bool
	references        bool
	declaration       bool
	typeDefinition    bool
	documentSymbol    bool
	callHierarchy     bool
	typeHierarchy     bool
	signatureHelp     bool
	documentHighlight bool
	inlayHint         bool
	executeCommands   []string
}

func snapshotCapabilities(capabilities *protocol.ServerCapabilities) sessionCapabilities {
	if capabilities == nil {
		return sessionCapabilities{}
	}
	out := sessionCapabilities{
		pullDiagnostics:   capabilities.DiagnosticProvider != nil,
		implementation:    implementationProviderSupported(capabilities.ImplementationProvider),
		hover:             providerSupported(capabilities.HoverProvider),
		codeAction:        providerSupported(capabilities.CodeActionProvider),
		codeLens:          capabilities.CodeLensProvider != nil,
		workspaceSymbol:   providerSupported(capabilities.WorkspaceSymbolProvider),
		formatting:        providerSupported(capabilities.DocumentFormattingProvider),
		rangeFormatting:   providerSupported(capabilities.DocumentRangeFormattingProvider),
		rename:            providerSupported(capabilities.RenameProvider),
		references:        providerSupported(capabilities.ReferencesProvider),
		declaration:       providerSupported(capabilities.DeclarationProvider),
		typeDefinition:    providerSupported(capabilities.TypeDefinitionProvider),
		documentSymbol:    providerSupported(capabilities.DocumentSymbolProvider),
		callHierarchy:     providerSupported(capabilities.CallHierarchyProvider),
		typeHierarchy:     providerSupported(capabilities.TypeHierarchyProvider),
		signatureHelp:     capabilities.SignatureHelpProvider != nil,
		documentHighlight: providerSupported(capabilities.DocumentHighlightProvider),
		inlayHint:         providerSupported(capabilities.InlayHintProvider),
		executeCommands:   slices.Clone(capabilities.ExecuteCommandProvider.Commands),
	}
	if opts, ok := capabilities.CodeActionProvider.(*protocol.CodeActionOptions); ok && opts != nil && opts.ResolveProvider != nil {
		out.codeActionResolve = *opts.ResolveProvider
	}
	if capabilities.CodeLensProvider != nil && capabilities.CodeLensProvider.ResolveProvider != nil {
		out.codeLensResolve = *capabilities.CodeLensProvider.ResolveProvider
	}
	return out
}

// start spawns the server, wires the jsonrpc2 connection, performs the
// initialize handshake, and records advertised server capabilities. It closes
// ready when finished, with initErr set on failure. It runs exactly once under
// the session's [sync.Once].
//
// The connection context is rooted with [context.WithoutCancel] so it
// keeps parent's values but is detached from a tool call's cancellation:
// otherwise canceling the first call would tear down the read loop and hang
// every later call on a dead connection.
func (s *serverSession) start(parent context.Context, cfg ServerConfig, rootURI uri.URI) {
	defer close(s.ready)

	sctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	s.cancel = cancel

	// The command and arguments come from the operator-controlled runtime
	// registry built from config, CLI override, or PATH discovery. MCP tool input
	// cannot provide this command, and it is executed without shell expansion.
	cmd := exec.CommandContext(sctx, cfg.Command, cfg.Args...) //nolint:gosec // operator-controlled runtime registry command, not tool input or shell text
	cmd.Stderr = newLogWriter(s.logger)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		s.failStart(fmt.Errorf("open language server stdin: %w", err))
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.failStart(fmt.Errorf("open language server stdout: %w", err))
		return
	}

	if err := cmd.Start(); err != nil {
		s.failStart(fmt.Errorf("start language server %q: %w", cfg.Command, err))
		return
	}
	s.cmd = cmd

	stream := jsonrpc2.NewHeaderStream(&pipeRWC{r: stdout, w: stdin})
	s.client = newClient(s.store, s.logger)
	_, conn, server := protocol.NewClient(sctx, s.client, stream)
	s.conn = conn
	s.server = server

	res, err := server.Initialize(sctx, initializeParams(rootURI))
	if err != nil {
		s.failStart(fmt.Errorf("language server initialize failed: %w", err))
		return
	}
	if err := server.Initialized(sctx, &protocol.InitializedParams{}); err != nil {
		s.failStart(fmt.Errorf("language server initialized failed: %w", err))
		return
	}
	s.capabilities = snapshotCapabilities(&res.Capabilities)
	s.pullSupported = s.capabilities.pullDiagnostics
	s.implementationSupported = s.capabilities.implementation
	s.openDocs = make(map[uri.URI]*openedDocument)

	go s.watch()
}

// failStart records an initialization failure and tears down everything the
// partially started session created. It is fully self-cleaning rather than
// relying on a later shutdown: a failed session is marked dead and is replaced
// wholesale by the [Manager] on the next request, which drops this pointer, so if
// failStart did not reap the child and close the pipes here they would leak (a
// zombie process plus its stdio descriptors) on every failed handshake.
//
// [jsonrpc2.Conn.Close] is called explicitly because, per the jsonrpc2 contract,
// canceling the context is observed only between frames: a reader already parked
// mid-frame is unblocked promptly only by closing the stream, not by ctx
// cancellation.
func (s *serverSession) failStart(err error) {
	s.initErr = err
	s.dead.Store(true)
	if s.conn != nil {
		_ = s.conn.Close()
	}
	if s.cancel != nil {
		s.cancel()
	}
	if s.cmd != nil {
		_ = s.waitProcess()
	}
}

// watch waits for the connection read loop to exit (the server died or was shut
// down), marks the session dead, and releases any diagnostics waiters so they
// stop blocking on data that will never arrive.
func (s *serverSession) watch() {
	<-s.conn.Done()
	s.dead.Store(true)
	if err := s.conn.Err(); err != nil {
		s.logger.Warn("language server connection closed", slog.Any("error", err))
	}
	s.store.broadcastAll()
}

// shutdown stops the session: a best-effort LSP shutdown/exit handshake, then
// closing the connection, waiting for the process to exit, and canceling the
// context. It kills the process if it overruns shutdownWait. It is idempotent.
func (s *serverSession) shutdown(ctx context.Context) error {
	s.shutdownOnce.Do(func() {
		s.shutdownErr = s.doShutdown(ctx)
	})
	return s.shutdownErr
}

func (s *serverSession) doShutdown(ctx context.Context) error {
	<-s.ready
	if s.cancel != nil {
		defer s.cancel()
	}

	// Bound the LSP handshake independently of the caller's context, which may
	// have no deadline ([Manager.Close] is invoked with a non-cancelable context
	// during process teardown). A wedged server that never answers shutdown/exit
	// would otherwise block here forever, since [jsonrpc2.Conn.Call] returns only
	// on a response, a write error, or ctx cancellation. [jsonrpc2.Conn.Close]
	// below then guarantees teardown even when the handshake timed out.
	if s.server != nil {
		hctx, cancel := context.WithTimeout(ctx, shutdownWait)
		s.closeOpenDocuments(hctx)
		if err := s.server.Shutdown(hctx); err != nil {
			s.logger.Debug("language server shutdown request failed", slog.Any("error", err))
		}
		if err := s.server.Exit(hctx); err != nil {
			s.logger.Debug("language server exit request failed", slog.Any("error", err))
		}
		cancel()
	}
	if s.conn != nil {
		_ = s.conn.Close()
	}
	return s.waitProcess()
}

// waitProcess waits for the subprocess to exit, killing it if it overruns
// shutdownWait.
func (s *serverSession) waitProcess() error {
	if s.cmd == nil {
		return nil
	}

	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()

	select {
	case err := <-done:
		if err != nil && !isCleanExit(err) {
			return fmt.Errorf("language server exited: %w", err)
		}
		return nil
	case <-time.After(shutdownWait):
		_ = s.cmd.Process.Kill()
		<-done
		return fmt.Errorf("language server did not exit within %s; killed", shutdownWait)
	}
}

// isCleanExit reports whether an [exec.Cmd.Wait] error is the expected
// consequence of a requested shutdown rather than a genuine failure.
func isCleanExit(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr)
}

// initializeParams builds the [protocol.InitializeParams] advertising the
// concrete client capabilities implemented by mcp-lsp, rooted at rootURI.
func initializeParams(rootURI uri.URI) *protocol.InitializeParams {
	pid := int32(os.Getpid()) //nolint:gosec // a process id fits in int32 on every supported platform
	return &protocol.InitializeParams{
		ProcessID: &pid,
		RootURI:   &rootURI,
		ClientInfo: protocol.ClientInfo{
			Name: "mcp-lsp",
		},
		WorkspaceFolders: protocol.NewNullable([]protocol.WorkspaceFolder{
			{URI: rootURI, Name: "workspace"},
		}),
		Capabilities: protocol.ClientCapabilities{
			Workspace: &protocol.WorkspaceClientCapabilities{
				ApplyEdit: new(true),
				WorkspaceEdit: &protocol.WorkspaceEditClientCapabilities{
					DocumentChanges: new(true),
				},
				Symbol:         &protocol.WorkspaceSymbolClientCapabilities{SymbolKind: &protocol.ClientSymbolKindOptions{ValueSet: supportedSymbolKinds()}},
				ExecuteCommand: &protocol.ExecuteCommandClientCapabilities{},
			},
			TextDocument: &protocol.TextDocumentClientCapabilities{
				Synchronization: &protocol.TextDocumentSyncClientCapabilities{
					DynamicRegistration: new(true),
				},
				Hover: &protocol.HoverClientCapabilities{
					ContentFormat: []protocol.MarkupKind{protocol.MarkupKindMarkdown, protocol.MarkupKindPlainText},
				},
				Definition: &protocol.DefinitionClientCapabilities{
					LinkSupport: new(true),
				},
				Implementation: &protocol.ImplementationClientCapabilities{
					LinkSupport: new(true),
				},
				Declaration: &protocol.DeclarationClientCapabilities{
					LinkSupport: new(true),
				},
				TypeDefinition: &protocol.TypeDefinitionClientCapabilities{
					LinkSupport: new(true),
				},
				References: &protocol.ReferenceClientCapabilities{},
				DocumentSymbol: &protocol.DocumentSymbolClientCapabilities{
					SymbolKind:                        &protocol.ClientSymbolKindOptions{ValueSet: supportedSymbolKinds()},
					HierarchicalDocumentSymbolSupport: new(true),
				},
				CallHierarchy:     &protocol.CallHierarchyClientCapabilities{},
				TypeHierarchy:     &protocol.TypeHierarchyClientCapabilities{},
				SignatureHelp:     &protocol.SignatureHelpClientCapabilities{},
				DocumentHighlight: &protocol.DocumentHighlightClientCapabilities{},
				InlayHint:         &protocol.InlayHintClientCapabilities{},
				CodeAction: &protocol.CodeActionClientCapabilities{
					CodeActionLiteralSupport: protocol.ClientCodeActionLiteralOptions{
						CodeActionKind: protocol.ClientCodeActionKindOptions{ValueSet: supportedCodeActionKinds()},
					},
					IsPreferredSupport: new(true),
					DisabledSupport:    new(true),
					DataSupport:        new(true),
					ResolveSupport: protocol.ClientCodeActionResolveOptions{
						Properties: []string{"edit", "command"},
					},
				},
				CodeLens: &protocol.CodeLensClientCapabilities{
					ResolveSupport: protocol.ClientCodeLensResolveOptions{Properties: []string{"command"}},
				},
				PublishDiagnostics: &protocol.PublishDiagnosticsClientCapabilities{
					RelatedInformation: new(true),
				},
				Diagnostic: &protocol.DiagnosticClientCapabilities{
					RelatedInformation:     new(true),
					RelatedDocumentSupport: new(true),
				},
				Formatting:      &protocol.DocumentFormattingClientCapabilities{},
				RangeFormatting: &protocol.DocumentRangeFormattingClientCapabilities{},
				Rename:          &protocol.RenameClientCapabilities{},
			},
		},
	}
}

func supportedCodeActionKinds() []protocol.CodeActionKind {
	return []protocol.CodeActionKind{
		protocol.CodeActionKindQuickFix,
		protocol.CodeActionKindRefactor,
		protocol.CodeActionKindRefactorExtract,
		protocol.CodeActionKindRefactorInline,
		protocol.CodeActionKindRefactorMove,
		protocol.CodeActionKindRefactorRewrite,
		protocol.CodeActionKindSource,
		protocol.CodeActionKindSourceOrganizeImports,
		protocol.CodeActionKindSourceFixAll,
		protocol.CodeActionKindNotebook,
	}
}

func supportedSymbolKinds() []protocol.SymbolKind {
	return []protocol.SymbolKind{
		protocol.SymbolKindFile,
		protocol.SymbolKindModule,
		protocol.SymbolKindNamespace,
		protocol.SymbolKindPackage,
		protocol.SymbolKindClass,
		protocol.SymbolKindMethod,
		protocol.SymbolKindProperty,
		protocol.SymbolKindField,
		protocol.SymbolKindConstructor,
		protocol.SymbolKindEnum,
		protocol.SymbolKindInterface,
		protocol.SymbolKindFunction,
		protocol.SymbolKindVariable,
		protocol.SymbolKindConstant,
		protocol.SymbolKindString,
		protocol.SymbolKindNumber,
		protocol.SymbolKindBoolean,
		protocol.SymbolKindArray,
		protocol.SymbolKindObject,
		protocol.SymbolKindKey,
		protocol.SymbolKindNull,
		protocol.SymbolKindEnumMember,
		protocol.SymbolKindStruct,
		protocol.SymbolKindEvent,
		protocol.SymbolKindOperator,
		protocol.SymbolKindTypeParameter,
	}
}

func implementationProviderSupported(provider protocol.ImplementationProvider) bool {
	return providerSupported(provider)
}

func providerSupported(provider any) bool {
	switch p := provider.(type) {
	case nil:
		return false
	case protocol.Boolean:
		return bool(p)
	}
	value := reflect.ValueOf(provider)
	return value.Kind() != reflect.Pointer || !value.IsNil()
}

// pipeRWC adapts the subprocess's separate stdout (read) and stdin (write)
// pipes into a single [io.ReadWriteCloser] for the jsonrpc2 header stream.
// [pipeRWC.Close] closes both ends.
type pipeRWC struct {
	r io.ReadCloser
	w io.WriteCloser
}

func (p *pipeRWC) Read(b []byte) (int, error) { return p.r.Read(b) }

func (p *pipeRWC) Write(b []byte) (int, error) { return p.w.Write(b) }

func (p *pipeRWC) Close() error {
	werr := p.w.Close()
	rerr := p.r.Close()
	if werr != nil {
		return werr
	}
	return rerr
}

// logWriter adapts the subprocess stderr to the structured logger, emitting one
// log record per write.
type logWriter struct {
	logger *slog.Logger
}

func newLogWriter(logger *slog.Logger) *logWriter {
	return &logWriter{logger: logger}
}

func (w *logWriter) Write(b []byte) (int, error) {
	w.logger.Debug("lsp stderr", slog.String("output", string(b)))
	return len(b), nil
}
