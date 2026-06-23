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
	"io"
	"sync/atomic"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// Client is a Language Server Protocol client connected to a single downstream
// language server. It owns the jsonrpc2 connection, the transport beneath it,
// and the document-version counter, and handles the LSP lifecycle (initialize,
// initialized, didOpen, shutdown via Close). Per-feature APIs are reached
// through accessors — diagnostics via [Client.Diagnostic] today, with hover,
// goto-definition, and references to follow as sibling features — so the LSP
// surface grows by adding feature types rather than methods on Client.
//
// The zero value is not usable; construct a Client with [NewClient] or
// [NewProcessClient]. A Client is safe for concurrent use; Close is idempotent.
type Client struct {
	conn      jsonrpc2.Conn
	transport io.ReadWriteCloser
	server    protocol.Server

	// diagnostic is the inbound push-model sink: the protocol.Client callback the
	// server pushes published diagnostics into. The outbound diagnostic API is the
	// [Diagnostic] feature returned by [Client.Diagnostic], which reads and
	// subscribes to this sink.
	diagnostic *pushSink

	// docVersion assigns each didOpen a distinct, monotonically increasing
	// version. LSP servers key incremental document state on the version, so
	// re-opening a file with a stale version can be silently ignored; an atomic
	// counter keeps concurrent and repeated collections from colliding.
	docVersion atomic.Int32
}

// NewProcessClient spawns name with args as a language server and returns a
// Client connected to it over the server's stdio, framed with the LSP base
// protocol. stderr receives the server's standard error stream and may be nil
// to discard it. opts configure the spawned process (working directory,
// environment).
//
// The returned Client owns the child process; Close tears it down. On a spawn
// or wiring failure no process or pipe is leaked.
func NewProcessClient(ctx context.Context, stderr io.Writer, name string, args []string, opts ...ServerOption) (*Client, error) {
	t, err := newProcessTransport(ctx, stderr, name, args, opts...)
	if err != nil {
		return nil, err
	}

	return newClient(ctx, t), nil
}

// NewClient returns a Client that speaks the LSP base protocol over rwc, a
// connected duplex stream to a language server. The Client owns rwc and closes
// it on Close. This constructor backs callers that already hold a transport,
// such as an in-memory pipe in tests or a socket established elsewhere.
func NewClient(ctx context.Context, rwc io.ReadWriteCloser) *Client {
	return newClient(ctx, newPipeTransport(rwc))
}

// newClient wires a transport into a fully connected Client: it builds the LSP
// header-framed jsonrpc2 stream, constructs the push-model sink, and starts the
// connection's read loop via [protocol.NewClient]. The sink is passed as the
// server->client callback, so the read loop is live on return and diagnostics
// published before the first request are still captured.
func newClient(ctx context.Context, rwc io.ReadWriteCloser) *Client {
	diagnostic := newPushSink()

	stream := jsonrpc2.NewStream(rwc)
	_, conn, server := protocol.NewClient(ctx, diagnostic, stream)

	return &Client{
		conn:       conn,
		transport:  rwc,
		server:     server,
		diagnostic: diagnostic,
	}
}

// Diagnostic returns the client's diagnostics feature, the entry point for both
// the push model (published diagnostics, watchers) and the pull model. It is one
// of the per-feature APIs a Client exposes; future LSP features (hover, goto
// definition, references) are reached through sibling accessors rather than
// methods flattened onto Client.
//
// The returned value is a lightweight handle over the client's connection and
// push sink; it holds no state of its own and is cheap to obtain per call.
func (c *Client) Diagnostic() *Diagnostic {
	return &Diagnostic{client: c, sink: c.diagnostic}
}

// Server returns the dispatcher for raw client->server LSP requests, for callers
// that need protocol surface a typed feature does not yet expose.
func (c *Client) Server() protocol.Server { return c.server }

// Initialize performs the LSP `initialize` handshake. capabilities advertises
// what the client supports; rootURI is the workspace root, or "" for none.
//
// LSP requires `initialize` to be the first request and to complete before any
// other request is sent. Initialize does not send `initialized`; call
// [Client.Initialized] once the handshake result has been processed.
//
// The workspace root is advertised through the rootUri field. The LSP
// specification deprecates rootUri in favor of workspaceFolders, but the
// vendored protocol type exposes no public constructor for the nullable
// workspaceFolders value, and every language server still honors rootUri for
// backward compatibility, so rootUri remains the portable choice here.
func (c *Client) Initialize(ctx context.Context, rootURI uri.URI, capabilities protocol.ClientCapabilities) (*protocol.InitializeResult, error) {
	params := &protocol.InitializeParams{}
	params.Capabilities = capabilities
	if rootURI != "" {
		workspaceFolder := protocol.WorkspaceFolder{
			URI:  rootURI,
			Name: rootURI.Path(),
		}
		params.WorkspaceFolders = protocol.NewNullable([]protocol.WorkspaceFolder{workspaceFolder})
	}

	return c.server.Initialize(ctx, params)
}

// Initialized sends the `initialized` notification that completes the handshake
// and unblocks the server for normal operation.
func (c *Client) Initialized(ctx context.Context) error {
	return c.server.Initialized(ctx, &protocol.InitializedParams{})
}

// Open notifies the server that a document is now open with the given content,
// via `textDocument/didOpen`. Opening a document is what prompts a server to
// compute and push diagnostics for it under the push model.
func (c *Client) Open(ctx context.Context, docURI uri.URI, languageID protocol.LanguageKind, version int32, text string) error {
	return c.server.DidOpen(ctx, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:        docURI,
			LanguageID: languageID,
			Version:    version,
			Text:       text,
		},
	})
}

// open notifies the server that docURI is open at the next document version,
// taken from the atomic counter so every open is distinct: LSP servers key
// incremental document state on the version, so re-opening a file with a stale
// version can be silently ignored. It is the version-managing helper the
// per-feature APIs (such as [Diagnostic.Collect]) use to open a document before
// querying it.
func (c *Client) open(ctx context.Context, docURI uri.URI, languageID protocol.LanguageKind, text string) error {
	return c.Open(ctx, docURI, languageID, c.docVersion.Add(1), text)
}

// Close performs an abrupt but clean teardown: it closes the jsonrpc2
// connection (draining in-flight work and closing the stream), then closes the
// transport (reaping the process for a spawned server). It is safe to call more
// than once and reports the combined teardown error.
//
// For a graceful protocol shutdown, send `shutdown` then `exit` through
// [Client.Server] before calling Close.
func (c *Client) Close() error {
	// Close the connection first so its read loop stops and the stream is closed
	// at a frame boundary; only then tear down the transport beneath it.
	connErr := c.conn.Close()
	transportErr := c.transport.Close()

	return errors.Join(connErr, transportErr)
}

// Done returns a channel closed when the underlying connection has fully
// terminated, whether through Close or a transport failure.
func (c *Client) Done() <-chan struct{} { return c.conn.Done() }
