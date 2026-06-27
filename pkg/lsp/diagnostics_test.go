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
	"testing"
	"time"

	gocmp "github.com/google/go-cmp/cmp"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// fakeDiagnostics wraps a wired session so [Diagnostics.Lookup] can drive it
// without a real [Manager.session] spawn.
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
	if diff := gocmp.Diff(want, got); diff != "" {
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
	if diff := gocmp.Diff(want, got); diff != "" {
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
	if diff := gocmp.Diff(want, flattenDiagnostics(raw)); diff != "" {
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
	if diff := gocmp.Diff(want, got); diff != "" {
		t.Errorf("Lookup diagnostics mismatch (-want +got):\n%s", diff)
	}
}
