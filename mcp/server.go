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

// Package mcp exposes a downstream language server's diagnostics to agents
// through a Model Context Protocol server. It wraps the LSP client in the lsp
// package with a single typed tool, lsp_diagnostics, that an agent calls to
// collect the diagnostics for a file using either LSP diagnostic model.
//
// This file owns the MCP protocol surface: server construction, the LSP
// handshake, tool registration, and the serve and one-shot entry points. The
// diagnostic tool's schema and dispatch live in diagnostic.go, and the
// diagnostic collection itself lives in the lsp package, so this layer stays
// concerned only with MCP.
package mcp

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/zchee/mcp-lsp/internal/version"
	"github.com/zchee/mcp-lsp/lsp"
)

// toolName is the MCP tool an agent calls to collect a file's diagnostics.
const toolName = "lsp_diagnostics"

// Server is an MCP server that exposes a single downstream language server to
// agents. It owns the MCP protocol surface; the LSP client and workspace root
// it drives are supplied at construction and shared across every tool call.
type Server struct {
	srv     *mcp.Server
	client  *lsp.Client
	rootURI uri.URI
}

// NewServer performs the LSP handshake on client and returns an MCP server that
// exposes the language server's diagnostics through the lsp_diagnostics tool.
// rootURI is advertised as the workspace root; logger receives MCP server
// activity and may be the discard logger.
//
// The LSP initialize/initialized handshake runs here, once, so the server is
// ready to serve tool calls the moment Serve is invoked.
func NewServer(ctx context.Context, client *lsp.Client, rootURI uri.URI, logger *slog.Logger) (*Server, error) {
	clientCapabilities := protocol.ClientCapabilities{
		TextDocument: &protocol.TextDocumentClientCapabilities{
			PublishDiagnostics: &protocol.PublishDiagnosticsClientCapabilities{},
			Diagnostic:         &protocol.DiagnosticClientCapabilities{},
		},
	}
	if _, err := client.Initialize(ctx, rootURI, clientCapabilities); err != nil {
		return nil, fmt.Errorf("initialize: %w", err)
	}
	if err := client.Initialized(ctx); err != nil {
		return nil, fmt.Errorf("initialized: %w", err)
	}

	imp := &mcp.Implementation{
		Name:       "mcp-lsp",
		Version:    version.Version,
		WebsiteURL: "https://github.com/zchee/mcp-lsp",
	}
	opts := &mcp.ServerOptions{
		Logger: logger,
		GetSessionID: func() string {
			return uuid.Must(uuid.NewV7()).String()
		},
	}

	s := &Server{
		srv:     mcp.NewServer(imp, opts),
		client:  client,
		rootURI: rootURI,
	}

	tool := &mcp.Tool{
		Name:        toolName,
		Title:       "LSP diagnostics",
		Description: "Collect the diagnostics a language server reports for a file, using either the push model (wait for published diagnostics) or the pull model (textDocument/diagnostic).",
		Annotations: &mcp.ToolAnnotations{
			// The tool only reads the file and queries the server; it never
			// mutates the workspace.
			ReadOnlyHint: true,
		},
	}
	// The typed AddTool infers the tool's input and output schemas from
	// DiagnosticInput and DiagnosticOutput, unmarshals and validates the
	// arguments, and packs the returned output into the structured result.
	mcp.AddTool(s.srv, tool, s.handleDiagnostic)

	return s, nil
}

// Serve runs the MCP server over stdin/stdout until the transport closes or ctx
// is cancelled. It is the entry point an agent connects to.
func (s *Server) Serve(ctx context.Context) error {
	if err := s.srv.Run(ctx, &mcp.StdioTransport{}); err != nil {
		return fmt.Errorf("serving mcp over stdio: %w", err)
	}
	return nil
}

// RunOnce collects a single file's diagnostics and prints them to w, without
// standing up the MCP transport. It is the one-shot CLI path and runs the same
// collection logic the lsp_diagnostics tool exposes, so the two modes cannot
// diverge. mode is "push" or "pull"; an empty mode defaults to push.
func (s *Server) RunOnce(ctx context.Context, w io.Writer, in DiagnosticInput) error {
	_, out, err := s.handleDiagnostic(ctx, nil, in)
	if err != nil {
		return err
	}
	printDiagnostics(w, out)
	return nil
}

// printDiagnostics renders a collected report to w, one diagnostic per line, in
// the conventional "line:col: severity: message" form.
func printDiagnostics(w io.Writer, out DiagnosticOutput) {
	if out.Unchanged {
		fmt.Fprintf(w, "%s: unchanged\n", out.URI)
		return
	}
	if len(out.Diagnostics) == 0 {
		fmt.Fprintf(w, "%s: no diagnostics\n", out.URI)
		return
	}

	fmt.Fprintf(w, "%s: %d diagnostic(s)\n", out.URI, len(out.Diagnostics))
	for _, d := range out.Diagnostics {
		fmt.Fprintf(w, "  %d:%d: %s: %s\n", d.Line, d.Column, d.Severity, d.Message)
	}
}
