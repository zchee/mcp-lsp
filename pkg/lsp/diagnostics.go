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
	"time"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// Default acquisition timings for the push (settle) read path.
const defaultSettle = 250 * time.Millisecond

// Diagnostic is the language-agnostic diagnostics DTO that drives the MCP tool
// output schema. Positions are zero-based here, matching the LSP wire format;
// the MCP layer converts them to one-based for the agent.
type Diagnostic struct {
	StartLine   int
	StartColumn int
	EndLine     int
	EndColumn   int
	Severity    string
	Source      string
	Code        string
	Message     string
}

// Diagnostics looks up language-server diagnostics through a [Manager]. settle is how long
// the push path waits for the publish stream to go quiet; timeout bounds the
// whole acquisition when the caller's context has no deadline.
type Diagnostics struct {
	mgr     *Manager
	settle  time.Duration
	timeout time.Duration
}

// Diagnostics returns the diagnostics helper for this manager.
func (m *Manager) Diagnostics() *Diagnostics {
	return &Diagnostics{
		mgr:     m,
		settle:  defaultSettle,
		timeout: defaultTimeout,
	}
}

// Lookup opens absPath in the language server for lang with the caller-supplied
// text and returns its current diagnostics. When the server advertises pull
// diagnostics it issues a textDocument/diagnostic request; otherwise it waits
// for the push (publishDiagnostics) stream to settle. Positions in the result
// are zero-based.
func (d *Diagnostics) Lookup(ctx context.Context, lang, absPath, text string) ([]Diagnostic, error) {
	sess, err := d.mgr.session(ctx, lang)
	if err != nil {
		return nil, err
	}

	cfg := d.mgr.cfg[lang]
	u := uri.File(absPath)

	baselineSeq := sess.store.publishSeq(u)
	if err := sess.syncTextDocument(ctx, u, cfg.LanguageID, text); err != nil {
		return nil, err
	}

	diags, err := d.acquire(ctx, sess, u, baselineSeq)
	if err != nil {
		return nil, err
	}
	return flattenDiagnostics(diags), nil
}

// acquire returns [protocol.Diagnostic] values for u, using the pull path when
// the session supports it and falling back to the cached push stream otherwise.
func (d *Diagnostics) acquire(ctx context.Context, sess *serverSession, u uri.URI, baselineSeq uint64) ([]protocol.Diagnostic, error) {
	if !sess.pullSupported {
		ctx, cancel := d.withTimeout(ctx)
		defer cancel()
		return sess.store.waitSettledAfter(ctx, u, d.settle, baselineSeq)
	}

	rep, err := sess.server.Diagnostic(ctx, docDiagParams(u))
	if err != nil {
		return nil, fmt.Errorf("pull diagnostics: %w", err)
	}

	switch report := rep.(type) {
	case *protocol.RelatedFullDocumentDiagnosticReport:
		return report.Items, nil
	case *protocol.RelatedUnchangedDocumentDiagnosticReport:
		// The server reports the previous result is still accurate; serve the
		// authoritative push cache.
		diags, _ := sess.store.snapshot(u)
		return diags, nil
	default:
		diags, _ := sess.store.snapshot(u)
		return diags, nil
	}
}

// withTimeout derives a context with the diagnostics timeout when the parent
// carries no deadline, so the push wait cannot block indefinitely.
func (d *Diagnostics) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d.timeout)
}

// flattenDiagnostics converts [protocol.Diagnostic] values into the domain DTO,
// flattening the union-typed message and code fields. Positions stay zero-based.
func flattenDiagnostics(in []protocol.Diagnostic) []Diagnostic {
	out := make([]Diagnostic, 0, len(in))
	for i := range in {
		d := &in[i]
		out = append(out, Diagnostic{
			StartLine:   int(d.Range.Start.Line),
			StartColumn: int(d.Range.Start.Character),
			EndLine:     int(d.Range.End.Line),
			EndColumn:   int(d.Range.End.Character),
			Severity:    severityString(d.Severity),
			Source:      optString(d.Source),
			Code:        codeString(d.Code),
			Message:     messageText(d.Message),
		})
	}
	return out
}

// didOpenParams builds a textDocument/didOpen notification for u with the given
// language and contents.
func didOpenParams(u uri.URI, lang protocol.LanguageKind, text string) *protocol.DidOpenTextDocumentParams {
	return &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:        u,
			LanguageID: lang,
			Version:    1,
			Text:       text,
		},
	}
}

// docDiagParams builds a textDocument/diagnostic pull request for u.
func docDiagParams(u uri.URI) *protocol.DocumentDiagnosticParams {
	return &protocol.DocumentDiagnosticParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: u},
	}
}
