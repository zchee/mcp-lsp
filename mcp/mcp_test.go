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
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-json-experiment/json"
	"github.com/google/go-cmp/cmp"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/zchee/mcp-lsp/lsp"
)

// discardLogger returns a logger that drops all records, keeping test output
// clean while satisfying the non-nil logger NewServer hands to the SDK.
func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// toolErrorText concatenates the text content of a tool-error result so a test
// can assert on the message the handler returned.
func toolErrorText(res *sdk.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// fakeLSPServer is an in-memory language server that drives the MCP diagnostic
// tool end to end. It embeds [protocol.UnimplementedServer] and overrides only
// the methods the diagnostic flows exercise, so unrelated requests answer with
// the standard "not implemented" error.
//
// Push behavior is configured by onDidOpen: opening a document publishes the
// configured diagnostics back through the server->client dispatcher. Pull
// behavior is configured by pullReport, returned verbatim from
// textDocument/diagnostic.
type fakeLSPServer struct {
	protocol.UnimplementedServer

	// client is the server->client dispatcher, set once the connection exists. It
	// is how the fake server pushes publishDiagnostics notifications.
	client protocol.Client

	// onDidOpen maps a document URI to the diagnostics the server publishes when
	// that document is opened. A missing entry publishes nothing.
	onDidOpen map[uri.URI][]protocol.Diagnostic

	// pullReport is returned from textDocument/diagnostic.
	pullReport protocol.DocumentDiagnosticReport
}

func (s *fakeLSPServer) Initialize(ctx context.Context, params *protocol.InitializeParams) (*protocol.InitializeResult, error) {
	return &protocol.InitializeResult{
		Capabilities: protocol.ServerCapabilities{},
	}, nil
}

func (s *fakeLSPServer) Initialized(ctx context.Context, params *protocol.InitializedParams) error {
	return nil
}

func (s *fakeLSPServer) DidOpen(ctx context.Context, params *protocol.DidOpenTextDocumentParams) error {
	docURI := params.TextDocument.URI
	diags, ok := s.onDidOpen[docURI]
	if !ok {
		return nil
	}

	return s.client.PublishDiagnostics(ctx, &protocol.PublishDiagnosticsParams{
		URI:         docURI,
		Version:     protocol.NewOptional(params.TextDocument.Version),
		Diagnostics: diags,
	})
}

func (s *fakeLSPServer) Diagnostic(ctx context.Context, params *protocol.DocumentDiagnosticParams) (protocol.DocumentDiagnosticReport, error) {
	return s.pullReport, nil
}

// newLSPClient stands up a real lsp.Client connected to srv over an in-memory
// pipe, wiring both ends with the real protocol dispatchers and header framing.
// It registers cleanup that tears both ends down.
func newLSPClient(t *testing.T, srv *fakeLSPServer) *lsp.Client {
	t.Helper()

	clientConn, serverConn := net.Pipe()

	serverStream := jsonrpc2.NewStream(serverConn)
	_, srvJSONConn, client := protocol.NewServer(t.Context(), srv, serverStream)
	srv.client = client

	c := lsp.NewClient(t.Context(), clientConn)

	t.Cleanup(func() {
		_ = c.Close()
		_ = srvJSONConn.Close()
	})

	return c
}

// newToolSession constructs the diagnostic MCP server over lspClient and returns
// an MCP client session connected to it through an in-memory transport, so tests
// drive the tool exactly as a live agent would: through CallTool, with full
// schema validation and structured-output marshaling.
func newToolSession(t *testing.T, lspClient *lsp.Client, rootURI uri.URI) *sdk.ClientSession {
	t.Helper()

	srv, err := NewServer(t.Context(), lspClient, rootURI, discardLogger())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	clientT, serverT := sdk.NewInMemoryTransports()
	if _, err := srv.srv.Connect(t.Context(), serverT, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}

	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	session, err := client.Connect(t.Context(), clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	return session
}

// callDiagnostic invokes the lsp_diagnostics tool and decodes its structured
// output into a DiagnosticOutput.
func callDiagnostic(t *testing.T, session *sdk.ClientSession, args map[string]any) (*sdk.CallToolResult, DiagnosticOutput) {
	t.Helper()

	res, err := session.CallTool(t.Context(), &sdk.CallToolParams{
		Name:      toolName,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	var out DiagnosticOutput
	if res.StructuredContent != nil {
		raw, err := json.Marshal(res.StructuredContent)
		if err != nil {
			t.Fatalf("marshaling structured content: %v", err)
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("unmarshaling structured content: %v", err)
		}
	}

	return res, out
}

// writeTempGo writes content to a .go file in a temp dir and returns its
// absolute path and the directory's URI, suitable as a workspace root.
func writeTempGo(t *testing.T, content string) (string, uri.URI) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	return path, uri.File(dir)
}

func TestDiagnostic_Push(t *testing.T) {
	t.Parallel()

	mkDiag := func(msg string, line uint32, sev protocol.DiagnosticSeverity) protocol.Diagnostic {
		return protocol.Diagnostic{
			Range: protocol.Range{
				Start: protocol.Position{Line: line, Character: 2},
				End:   protocol.Position{Line: line, Character: 6},
			},
			Severity: sev,
			Source:   protocol.NewOptional("test"),
			Message:  protocol.String(msg),
		}
	}

	tests := map[string]struct {
		publish []protocol.Diagnostic
		want    []Diagnostic
	}{
		"success: single error diagnostic": {
			publish: []protocol.Diagnostic{
				mkDiag("undeclared name: x", 3, protocol.DiagnosticSeverityError),
			},
			want: []Diagnostic{
				{Line: 4, Column: 3, EndLine: 4, EndColumn: 7, Severity: "error", Source: "test", Message: "undeclared name: x"},
			},
		},
		"success: multiple diagnostics": {
			publish: []protocol.Diagnostic{
				mkDiag("error one", 1, protocol.DiagnosticSeverityError),
				mkDiag("warning two", 5, protocol.DiagnosticSeverityWarning),
			},
			want: []Diagnostic{
				{Line: 2, Column: 3, EndLine: 2, EndColumn: 7, Severity: "error", Source: "test", Message: "error one"},
				{Line: 6, Column: 3, EndLine: 6, EndColumn: 7, Severity: "warning", Source: "test", Message: "warning two"},
			},
		},
		"success: clean document": {
			publish: []protocol.Diagnostic{},
			want:    []Diagnostic{},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path, rootURI := writeTempGo(t, "package main")
			docURI := uri.File(path)

			srv := &fakeLSPServer{
				onDidOpen: map[uri.URI][]protocol.Diagnostic{
					docURI: tt.publish,
				},
			}
			lspClient := newLSPClient(t, srv)
			session := newToolSession(t, lspClient, rootURI)

			res, out := callDiagnostic(t, session, map[string]any{
				"file":        path,
				"mode":        "push",
				"waitSeconds": 5,
			})

			if res.IsError {
				t.Fatalf("tool reported error: %+v", res.Content)
			}
			if out.URI != string(docURI) {
				t.Errorf("URI = %q, want %q", out.URI, docURI)
			}
			if out.Mode != "push" {
				t.Errorf("Mode = %q, want push", out.Mode)
			}
			if diff := cmp.Diff(tt.want, out.Diagnostics); diff != "" {
				t.Errorf("diagnostics mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDiagnostic_Pull(t *testing.T) {
	t.Parallel()

	resultID := "rev-1"

	tests := map[string]struct {
		report        protocol.DocumentDiagnosticReport
		wantUnchanged bool
		want          []Diagnostic
	}{
		"success: full report with items": {
			report: &protocol.RelatedFullDocumentDiagnosticReport{
				Kind:     string(protocol.DocumentDiagnosticReportKindFull),
				ResultID: &resultID,
				Items: []protocol.Diagnostic{
					{
						Range:    protocol.Range{Start: protocol.Position{Line: 0, Character: 0}, End: protocol.Position{Line: 0, Character: 4}},
						Severity: protocol.DiagnosticSeverityError,
						Message:  protocol.String("pulled error"),
					},
				},
			},
			want: []Diagnostic{
				{Line: 1, Column: 1, EndLine: 1, EndColumn: 5, Severity: "error", Message: "pulled error"},
			},
		},
		"success: unchanged report": {
			report: &protocol.RelatedUnchangedDocumentDiagnosticReport{
				Kind:     string(protocol.DocumentDiagnosticReportKindUnchanged),
				ResultID: "rev-1",
			},
			wantUnchanged: true,
			want:          []Diagnostic{},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path, rootURI := writeTempGo(t, "package main")
			docURI := uri.File(path)

			srv := &fakeLSPServer{pullReport: tt.report}
			lspClient := newLSPClient(t, srv)
			session := newToolSession(t, lspClient, rootURI)

			res, out := callDiagnostic(t, session, map[string]any{
				"file": path,
				"mode": "pull",
			})

			if res.IsError {
				t.Fatalf("tool reported error: %+v", res.Content)
			}
			if out.Mode != "pull" {
				t.Errorf("Mode = %q, want pull", out.Mode)
			}
			if out.Unchanged != tt.wantUnchanged {
				t.Errorf("Unchanged = %v, want %v", out.Unchanged, tt.wantUnchanged)
			}
			if out.URI != string(docURI) {
				t.Errorf("URI = %q, want %q", out.URI, docURI)
			}
			if diff := cmp.Diff(tt.want, out.Diagnostics); diff != "" {
				t.Errorf("diagnostics mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDiagnostic_Errors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		// useRealFile substitutes the temp file path for the "file" argument so
		// the request clears schema validation and reaches the handler. Cases
		// that omit it exercise the schema gate instead.
		useRealFile bool
		args        map[string]any
		wantErrText string
	}{
		"error: missing file is invalid input": {
			args:        map[string]any{"mode": "push"},
			wantErrText: "",
		},
		"error: unknown mode": {
			useRealFile: true,
			args:        map[string]any{"mode": "sideways"},
			wantErrText: "unknown mode",
		},
		"error: nonexistent file": {
			args:        map[string]any{"file": "/nonexistent/path/does-not-exist.go"},
			wantErrText: "reading",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path, rootURI := writeTempGo(t, "package main")

			srv := &fakeLSPServer{}
			lspClient := newLSPClient(t, srv)
			session := newToolSession(t, lspClient, rootURI)

			if tt.useRealFile {
				tt.args["file"] = path
			}

			res, err := session.CallTool(t.Context(), &sdk.CallToolParams{
				Name:      toolName,
				Arguments: tt.args,
			})

			// A missing required field is rejected by schema validation as a
			// protocol error (err != nil); a handler-returned error is packed
			// into the result with IsError set. Either way the call must not
			// succeed cleanly.
			switch {
			case err != nil:
				// Schema validation rejected the input before the handler ran.
			case res.IsError:
				if tt.wantErrText != "" {
					text := toolErrorText(res)
					if !strings.Contains(text, tt.wantErrText) {
						t.Errorf("error text = %q, want it to contain %q", text, tt.wantErrText)
					}
				}
			default:
				t.Fatalf("expected an error, got clean result: %+v", res)
			}
		})
	}
}

func TestServer_RunOnce(t *testing.T) {
	t.Parallel()

	path, rootURI := writeTempGo(t, "package main")
	docURI := uri.File(path)

	srv := &fakeLSPServer{
		onDidOpen: map[uri.URI][]protocol.Diagnostic{
			docURI: {
				{
					Range:    protocol.Range{Start: protocol.Position{Line: 0, Character: 0}, End: protocol.Position{Line: 0, Character: 3}},
					Severity: protocol.DiagnosticSeverityWarning,
					Message:  protocol.String("once warning"),
				},
			},
		},
	}
	lspClient := newLSPClient(t, srv)

	server, err := NewServer(t.Context(), lspClient, rootURI, discardLogger())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	var buf strings.Builder
	if err := server.RunOnce(t.Context(), &buf, DiagnosticInput{
		File:        path,
		Mode:        "push",
		WaitSeconds: 5,
	}); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "1 diagnostic(s)") {
		t.Errorf("output = %q, want it to report one diagnostic", got)
	}
	if !strings.Contains(got, "1:1: warning: once warning") {
		t.Errorf("output = %q, want it to contain the rendered diagnostic", got)
	}
}
