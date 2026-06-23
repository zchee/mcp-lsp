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
	"net"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// fakeServer is an in-memory language server used to drive the diagnostic
// client end to end. It embeds [protocol.UnimplementedServer] and overrides
// only the methods the diagnostic flows exercise, so unrelated requests answer
// with the standard "not implemented" error.
//
// The push-model behavior is configured by onDidOpen: when a document is
// opened the server publishes the given diagnostics back to the client through
// the server->client [protocol.Client] dispatcher. The pull-model behavior is
// configured by pullReport, returned verbatim from textDocument/diagnostic.
type fakeServer struct {
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

func (s *fakeServer) Initialize(_ context.Context, _ *protocol.InitializeParams) (*protocol.InitializeResult, error) {
	return &protocol.InitializeResult{
		Capabilities: protocol.ServerCapabilities{},
	}, nil
}

func (s *fakeServer) Initialized(_ context.Context, _ *protocol.InitializedParams) error {
	return nil
}

func (s *fakeServer) DidOpen(ctx context.Context, params *protocol.DidOpenTextDocumentParams) error {
	docURI := params.TextDocument.URI
	diags, ok := s.onDidOpen[docURI]
	if !ok {
		return nil
	}

	// Publishing from within a handler is a server->client notification, which
	// needs no Async release: Notify only writes and never awaits a response.
	return s.client.PublishDiagnostics(ctx, &protocol.PublishDiagnosticsParams{
		URI:         docURI,
		Version:     protocol.NewOptional(params.TextDocument.Version),
		Diagnostics: diags,
	})
}

func (s *fakeServer) Diagnostic(_ context.Context, _ *protocol.DocumentDiagnosticParams) (protocol.DocumentDiagnosticReport, error) {
	return s.pullReport, nil
}

// newTestClient stands up a diagnostic client connected to srv over an
// in-memory pipe, wiring both ends with the real protocol dispatchers and
// header framing. It registers cleanup that tears both ends down.
func newTestClient(t *testing.T, srv *fakeServer) *Client {
	t.Helper()

	clientConn, serverConn := net.Pipe()

	// Server end: serve the fake server and capture the server->client dispatcher
	// it uses to push diagnostics.
	serverStream := jsonrpc2.NewStream(serverConn)
	_, srvJSONConn, client := protocol.NewServer(t.Context(), srv, serverStream)
	srv.client = client

	// Client end: the diagnostic client under test.
	c := NewClient(t.Context(), clientConn)

	t.Cleanup(func() {
		_ = c.Close()
		_ = srvJSONConn.Close()
	})

	return c
}

// initClient stands up a client over srv and completes the LSP handshake, the
// precondition Collect assumes. It reuses the in-package fake server
// harness so the collection path is exercised against the real protocol
// dispatchers.
func initClient(t *testing.T, srv *fakeServer) *Client {
	t.Helper()

	c := newTestClient(t, srv)
	if _, err := c.Initialize(t.Context(), "", protocol.ClientCapabilities{}); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := c.Initialized(t.Context()); err != nil {
		t.Fatalf("Initialized: %v", err)
	}

	return c
}

// diagnosticsJSON marshals diagnostics to their canonical LSP JSON form. The
// protocol diagnostic types embed sealed-union and Optional[T] wrappers with
// unexported fields, which go-cmp refuses to traverse; comparing the wire
// encoding instead asserts the exact contract that matters for a client — that
// the diagnostics survived the round trip byte for byte — and is immune to the
// internal representation of every wrapper.
func diagnosticsJSON(t *testing.T, diags []protocol.Diagnostic) string {
	t.Helper()

	b, err := protocol.Marshal(diags)
	if err != nil {
		t.Fatalf("marshaling diagnostics: %v", err)
	}

	return string(b)
}

func TestClientCollect_DefaultModeIsPush(t *testing.T) {
	t.Parallel()

	docURI := uri.URI("file:///default.go")
	srv := &fakeServer{
		onDidOpen: map[uri.URI][]protocol.Diagnostic{
			docURI: {
				{
					Range:    protocol.Range{Start: protocol.Position{Line: 0, Character: 0}, End: protocol.Position{Line: 0, Character: 1}},
					Severity: protocol.DiagnosticSeverityWarning,
					Message:  protocol.String("defaulted"),
				},
			},
		},
	}
	c := initClient(t, srv)

	// An empty mode must behave as push: open the document and collect the
	// published set.
	report, err := c.Diagnostic().Collect(t.Context(), docURI, protocol.LanguageKindGo, "package main", "", 5*time.Second)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if report.Mode != ModePush {
		t.Errorf("Mode = %q, want %q for empty mode", report.Mode, ModePush)
	}
	if len(report.Diagnostics) != 1 {
		t.Fatalf("len(Diagnostics) = %d, want 1", len(report.Diagnostics))
	}
}

func TestClientCollect_Pull(t *testing.T) {
	t.Parallel()

	resultID := "rev-1"

	tests := map[string]struct {
		report        protocol.DocumentDiagnosticReport
		wantUnchanged bool
		want          []FlatDiagnostic
	}{
		"success: full report flattened": {
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
			want: []FlatDiagnostic{
				{Line: 1, Column: 1, EndLine: 1, EndColumn: 5, Severity: "error", Message: "pulled error"},
			},
		},
		"success: unchanged report carries no items": {
			report: &protocol.RelatedUnchangedDocumentDiagnosticReport{
				Kind:     string(protocol.DocumentDiagnosticReportKindUnchanged),
				ResultID: resultID,
			},
			wantUnchanged: true,
			want:          []FlatDiagnostic{},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := &fakeServer{pullReport: tt.report}
			c := initClient(t, srv)

			report, err := c.Diagnostic().Collect(t.Context(), uri.URI("file:///pull.go"), protocol.LanguageKindGo, "package main", ModePull, 0)
			if err != nil {
				t.Fatalf("Collect: %v", err)
			}

			if report.Mode != ModePull {
				t.Errorf("Mode = %q, want %q", report.Mode, ModePull)
			}
			if report.Unchanged != tt.wantUnchanged {
				t.Errorf("Unchanged = %v, want %v", report.Unchanged, tt.wantUnchanged)
			}
			if diff := cmp.Diff(tt.want, report.Diagnostics); diff != "" {
				t.Errorf("diagnostics mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestClientCollect_Push(t *testing.T) {
	t.Parallel()

	mkDiag := func(msg string, line uint32, sev protocol.DiagnosticSeverity, src string) protocol.Diagnostic {
		d := protocol.Diagnostic{
			Range: protocol.Range{
				Start: protocol.Position{Line: line, Character: 2},
				End:   protocol.Position{Line: line, Character: 6},
			},
			Severity: sev,
			Message:  protocol.String(msg),
		}
		if src != "" {
			d.Source = protocol.NewOptional(src)
		}
		return d
	}

	docURI := uri.URI("file:///push.go")

	tests := map[string]struct {
		publish []protocol.Diagnostic
		want    []FlatDiagnostic
	}{
		"success: clean document reports no diagnostics": {
			publish: []protocol.Diagnostic{},
			want:    []FlatDiagnostic{},
		},
		"success: severities mapped across kinds": {
			publish: []protocol.Diagnostic{
				mkDiag("warn", 1, protocol.DiagnosticSeverityWarning, ""),
				mkDiag("info", 2, protocol.DiagnosticSeverityInformation, ""),
				mkDiag("hint", 3, protocol.DiagnosticSeverityHint, ""),
			},
			want: []FlatDiagnostic{
				{Line: 2, Column: 3, EndLine: 2, EndColumn: 7, Severity: "warning", Message: "warn"},
				{Line: 3, Column: 3, EndLine: 3, EndColumn: 7, Severity: "info", Message: "info"},
				{Line: 4, Column: 3, EndLine: 4, EndColumn: 7, Severity: "hint", Message: "hint"},
			},
		},
		"success: single error flattened to one-based": {
			publish: []protocol.Diagnostic{
				mkDiag("undeclared name: x", 3, protocol.DiagnosticSeverityError, "compiler"),
			},
			want: []FlatDiagnostic{
				{Line: 4, Column: 3, EndLine: 4, EndColumn: 7, Severity: "error", Source: "compiler", Message: "undeclared name: x"},
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := &fakeServer{
				onDidOpen: map[uri.URI][]protocol.Diagnostic{docURI: tt.publish},
			}
			c := initClient(t, srv)

			report, err := c.Diagnostic().Collect(t.Context(), docURI, protocol.LanguageKindGo, "package main", ModePush, 5*time.Second)
			if err != nil {
				t.Fatalf("Collect: %v", err)
			}

			if report.URI != docURI {
				t.Errorf("URI = %q, want %q", report.URI, docURI)
			}
			if report.Mode != ModePush {
				t.Errorf("Mode = %q, want %q", report.Mode, ModePush)
			}
			if report.Unchanged {
				t.Errorf("Unchanged = true, want false for push mode")
			}
			if diff := cmp.Diff(tt.want, report.Diagnostics); diff != "" {
				t.Errorf("diagnostics mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestClientCollect_PushTimeoutIsClean(t *testing.T) {
	t.Parallel()

	// A server that publishes nothing for the opened document: the push wait must
	// elapse and report a clean document rather than failing.
	srv := &fakeServer{onDidOpen: map[uri.URI][]protocol.Diagnostic{}}
	c := initClient(t, srv)

	report, err := c.Diagnostic().Collect(t.Context(), uri.URI("file:///silent.go"), protocol.LanguageKindGo, "package main", ModePush, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if report.Mode != ModePush {
		t.Errorf("Mode = %q, want %q", report.Mode, ModePush)
	}
	if len(report.Diagnostics) != 0 {
		t.Errorf("Diagnostics = %v, want empty on timeout", report.Diagnostics)
	}
}

func TestClientCollect_UnknownMode(t *testing.T) {
	t.Parallel()

	srv := &fakeServer{}
	c := initClient(t, srv)

	_, err := c.Diagnostic().Collect(t.Context(), uri.URI("file:///x.go"), protocol.LanguageKindGo, "package main", "sideways", 0)
	if err == nil {
		t.Fatal("Collect with unknown mode = nil error, want an error")
	}
	if !strings.Contains(err.Error(), "unknown mode") {
		t.Errorf("error = %q, want it to mention %q", err, "unknown mode")
	}
}

func TestHandler_Pull(t *testing.T) {
	t.Parallel()

	resultID := "rev-1"

	tests := map[string]struct {
		report   protocol.DocumentDiagnosticReport
		wantKind protocol.DocumentDiagnosticReportKind
		wantLen  int
		wantID   string
	}{
		"success: full report with items": {
			report: &protocol.RelatedFullDocumentDiagnosticReport{
				Kind:     string(protocol.DocumentDiagnosticReportKindFull),
				ResultID: &resultID,
				Items: []protocol.Diagnostic{
					{
						Range:    protocol.Range{Start: protocol.Position{Line: 0}, End: protocol.Position{Line: 0, Character: 4}},
						Severity: protocol.DiagnosticSeverityError,
						Message:  protocol.String("pulled error"),
					},
				},
			},
			wantKind: protocol.DocumentDiagnosticReportKindFull,
			wantLen:  1,
			wantID:   "rev-1",
		},
		"success: unchanged report": {
			report: &protocol.RelatedUnchangedDocumentDiagnosticReport{
				Kind:     string(protocol.DocumentDiagnosticReportKindUnchanged),
				ResultID: "rev-1",
			},
			wantKind: protocol.DocumentDiagnosticReportKindUnchanged,
			wantLen:  0,
			wantID:   "rev-1",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := &fakeServer{pullReport: tt.report}
			c := newTestClient(t, srv)

			if _, err := c.Initialize(t.Context(), "", protocol.ClientCapabilities{}); err != nil {
				t.Fatalf("Initialize: %v", err)
			}

			report, err := c.Diagnostic().Pull(t.Context(), uri.URI("file:///a.go"), "", "")
			if err != nil {
				t.Fatalf("Pull: %v", err)
			}

			if report.Kind != tt.wantKind {
				t.Errorf("Kind = %q, want %q", report.Kind, tt.wantKind)
			}
			if len(report.Items) != tt.wantLen {
				t.Errorf("len(Items) = %d, want %d", len(report.Items), tt.wantLen)
			}
			if report.ResultID != tt.wantID {
				t.Errorf("ResultID = %q, want %q", report.ResultID, tt.wantID)
			}
		})
	}
}

func TestHandler_Push(t *testing.T) {
	t.Parallel()

	mkDiag := func(msg string, line uint32, sev protocol.DiagnosticSeverity) protocol.Diagnostic {
		return protocol.Diagnostic{
			Range: protocol.Range{
				Start: protocol.Position{Line: line, Character: 0},
				End:   protocol.Position{Line: line, Character: 1},
			},
			Severity: sev,
			Source:   protocol.NewOptional("test"),
			Message:  protocol.String(msg),
		}
	}

	tests := map[string]struct {
		docURI  uri.URI
		publish []protocol.Diagnostic
		want    []protocol.Diagnostic
		wantOK  bool
	}{
		"success: empty publish clears document": {
			docURI:  uri.URI("file:///c.go"),
			publish: []protocol.Diagnostic{},
			want:    nil,
			wantOK:  false,
		},
		"success: multiple diagnostics": {
			docURI: uri.URI("file:///b.go"),
			publish: []protocol.Diagnostic{
				mkDiag("error one", 1, protocol.DiagnosticSeverityError),
				mkDiag("warning two", 5, protocol.DiagnosticSeverityWarning),
			},
			want: []protocol.Diagnostic{
				mkDiag("error one", 1, protocol.DiagnosticSeverityError),
				mkDiag("warning two", 5, protocol.DiagnosticSeverityWarning),
			},
			wantOK: true,
		},
		"success: single error diagnostic": {
			docURI: uri.URI("file:///a.go"),
			publish: []protocol.Diagnostic{
				mkDiag("undeclared name: x", 3, protocol.DiagnosticSeverityError),
			},
			want: []protocol.Diagnostic{
				mkDiag("undeclared name: x", 3, protocol.DiagnosticSeverityError),
			},
			wantOK: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := &fakeServer{
				onDidOpen: map[uri.URI][]protocol.Diagnostic{
					tt.docURI: tt.publish,
				},
			}
			c := newTestClient(t, srv)

			if _, err := c.Initialize(t.Context(), "", protocol.ClientCapabilities{}); err != nil {
				t.Fatalf("Initialize: %v", err)
			}
			if err := c.Initialized(t.Context()); err != nil {
				t.Fatalf("Initialized: %v", err)
			}

			// Watch before opening so the publish triggered by didOpen cannot be
			// missed.
			events, cancel := c.Diagnostic().Watch(4)
			defer cancel()

			if err := c.Open(t.Context(), tt.docURI, protocol.LanguageKindGo, 1, "package a"); err != nil {
				t.Fatalf("Open: %v", err)
			}

			select {
			case event := <-events:
				if event.URI != tt.docURI {
					t.Fatalf("event URI = %q, want %q", event.URI, tt.docURI)
				}
				if diff := cmp.Diff(diagnosticsJSON(t, tt.publish), diagnosticsJSON(t, event.Diagnostics)); diff != "" {
					t.Errorf("watched diagnostics mismatch (-want +got):\n%s", diff)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("timed out waiting for published diagnostics")
			}

			got, ok := c.Diagnostic().Diagnostics(tt.docURI)
			if ok != tt.wantOK {
				t.Fatalf("Diagnostics ok = %v, want %v", ok, tt.wantOK)
			}
			if diff := cmp.Diff(diagnosticsJSON(t, tt.want), diagnosticsJSON(t, got)); diff != "" {
				t.Errorf("stored diagnostics mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPushSink_WatchDropsOldest(t *testing.T) {
	t.Parallel()

	s := newPushSink()

	// A capacity-one watcher: flooding it must not block, and the most recent
	// event must win.
	events, cancel := s.watch(1)
	defer cancel()

	const floods = 10
	var lastURI uri.URI
	for i := range floods {
		docURI := uri.URI("file:///" + string(rune('a'+i)) + ".go")
		lastURI = docURI
		if err := s.PublishDiagnostics(t.Context(), &protocol.PublishDiagnosticsParams{
			URI: docURI,
			Diagnostics: []protocol.Diagnostic{
				{Message: protocol.String("d")},
			},
		}); err != nil {
			t.Fatalf("PublishDiagnostics: %v", err)
		}
	}

	select {
	case event := <-events:
		if event.URI != lastURI {
			t.Errorf("buffered event URI = %q, want newest %q", event.URI, lastURI)
		}
	case <-time.After(time.Second):
		t.Fatal("expected a buffered event")
	}
}
