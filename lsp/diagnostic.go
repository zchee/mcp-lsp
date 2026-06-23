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
	"slices"
	"sync"
	"time"

	"github.com/go-json-experiment/json"
	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// DefaultPushWait bounds how long the push model waits for a server to
// volunteer diagnostics when a caller does not specify a window.
const DefaultPushWait = 10 * time.Second

// Mode selects the LSP diagnostic model used to collect a document's
// diagnostics.
type Mode string

const (
	// ModePush waits for the server to volunteer diagnostics through
	// `textDocument/publishDiagnostics` after the document is opened.
	ModePush Mode = "push"

	// ModePull requests diagnostics with the LSP 3.17 `textDocument/diagnostic`
	// request.
	ModePull Mode = "pull"
)

// FlatDiagnostic is a single LSP diagnostic flattened to the fields a consumer
// needs, with the LSP union types resolved and positions normalized. Positions
// are one-based to match how editors and humans refer to source locations,
// unlike the zero-based positions on the wire.
type FlatDiagnostic struct {
	// Line is the one-based line of the diagnostic's start position.
	Line uint32

	// Column is the one-based column of the diagnostic's start position.
	Column uint32

	// EndLine is the one-based line of the diagnostic's end position.
	EndLine uint32

	// EndColumn is the one-based column of the diagnostic's end position.
	EndColumn uint32

	// Severity is the textual severity: error, warning, info, hint, or unknown.
	Severity string

	// Source is the producer of the diagnostic (e.g. the linter or compiler
	// name), or "" when the server provided none.
	Source string

	// Message is the human-readable diagnostic text.
	Message string
}

// Report is the flattened result of a single diagnostic collection. It is the
// transport-neutral shape the lsp package hands back to a caller, independent of
// any presentation or protocol concern.
type Report struct {
	// URI is the file:// URI of the analyzed document.
	URI uri.URI

	// Mode is the diagnostic model that produced the result.
	Mode Mode

	// Unchanged is true only for a pull-model report where the server confirmed
	// the previous result is still accurate; Diagnostics is empty in that case.
	Unchanged bool

	// Diagnostics is the document's current diagnostic set, empty when the
	// document is clean.
	Diagnostics []FlatDiagnostic
}

// PublishedDiagnostics is a single push-model delivery: the diagnostics a server
// published for one document, as observed by a watcher.
type PublishedDiagnostics struct {
	// URI identifies the document the diagnostics apply to.
	URI uri.URI

	// Version is the document version the diagnostics were computed against, when
	// the server provided one.
	Version protocol.Optional[int32]

	// Diagnostics is the complete current diagnostic set for the document.
	Diagnostics []protocol.Diagnostic
}

// DiagnosticReport is the flattened result of a pull-model
// `textDocument/diagnostic` request. It collapses the LSP
// [protocol.DocumentDiagnosticReport] union into the fields a consumer
// actually needs: the report kind, the diagnostics for a full report, and the
// result id to feed into the next pull for the same document.
type DiagnosticReport struct {
	// Kind is [protocol.DocumentDiagnosticReportKindFull] when Items carries the
	// document's current diagnostics, or
	// [protocol.DocumentDiagnosticReportKindUnchanged] when the server reports
	// that the previous result is still accurate.
	Kind protocol.DocumentDiagnosticReportKind

	// Items holds the diagnostics for a full report. It is nil for an unchanged
	// report.
	Items []protocol.Diagnostic

	// ResultID is the server-provided result id, if any. Passing it back as the
	// previousResultId of the next pull lets the server answer "unchanged"
	// instead of recomputing.
	ResultID string
}

// rawDiagnosticReport is the permissive wire shape of a document diagnostic
// report. Both report arms (full and unchanged) are flattened into one struct so
// the response can be decoded without the strict union unmarshaler, which
// requires kind to be exactly "full" or "unchanged" and rejects the empty kind
// that some servers (notably gopls) emit for a full report.
type rawDiagnosticReport struct {
	Kind     string                `json:"kind"`
	ResultID string                `json:"resultId"`
	Items    []protocol.Diagnostic `json:"items"`
}

// Diagnostic is the diagnostics feature of a [Client]: the outbound API for both
// LSP diagnostic models. It is obtained from [Client.Diagnostic] and shares the
// client's connection; it is the sibling of the other per-feature APIs a Client
// exposes (hover, definition, and so on), each scoped to one slice of the LSP
// surface rather than flattened onto Client.
//
//   - Push: the server volunteers diagnostics through
//     `textDocument/publishDiagnostics`; [Diagnostic.Watch] subscribes to those
//     deliveries and [Diagnostic.Diagnostics] reads the latest stored set.
//   - Pull: [Diagnostic.Pull] requests diagnostics for a document with the LSP
//     3.17 `textDocument/diagnostic` request.
//
// [Diagnostic.Collect] drives either model end to end and returns a flattened
// [Report].
type Diagnostic struct {
	// client is the owning Client, the source of the connection used for pull
	// requests and document opens.
	client *Client

	// sink is the inbound push-model store the feature reads (Diagnostics) and
	// subscribes to (Watch). It is the same sink wired as the connection's
	// server->client callback.
	sink *pushSink
}

// Pull requests diagnostics for a document with the LSP 3.17
// `textDocument/diagnostic` request and returns the flattened report.
//
// previousResultID is the result id from an earlier pull for the same document,
// or "" on the first pull. Supplying it lets the server answer with an unchanged
// report instead of recomputing. identifier is the optional server-registered
// diagnostic identifier, or "" when none applies.
//
// The response is decoded leniently: the request asks for a raw result so the
// strict [protocol.DocumentDiagnosticReport] union unmarshaler is bypassed, then
// the report kind is interpreted tolerantly (see flattenRawReport). This
// accommodates servers that omit or empty the kind field for a full report,
// which the LSP 3.17 spec marks required but real servers do not always send.
func (d *Diagnostic) Pull(ctx context.Context, docURI uri.URI, identifier, previousResultID string) (*DiagnosticReport, error) {
	params := &protocol.DocumentDiagnosticParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: docURI},
	}
	if identifier != "" {
		params.Identifier = &identifier
	}
	if previousResultID != "" {
		params.PreviousResultID = &previousResultID
	}

	// Request the result as a raw message: the lspCodec copies the bytes verbatim
	// for a *jsonrpc2.RawMessage destination and never enters the union
	// dispatcher, leaving the lenient interpretation to flattenRawReport.
	var raw jsonrpc2.RawMessage
	if err := protocol.Call(ctx, d.client.conn, protocol.MethodTextDocumentDiagnostic, params, &raw); err != nil {
		return nil, err
	}

	return flattenRawReport(raw)
}

// Diagnostics returns a copy of the most recently published diagnostics for the
// given document. The boolean reports whether the server has published any
// diagnostics for the document; a published-then-cleared document reports false.
//
// This is the push model's stored view: it reflects whatever the server has
// most recently volunteered through `textDocument/publishDiagnostics`.
func (d *Diagnostic) Diagnostics(docURI uri.URI) ([]protocol.Diagnostic, bool) {
	return d.sink.diagnostics(docURI)
}

// Watch registers a watcher that receives every subsequent push-model delivery.
// It returns the receive channel and a cancel function that unregisters the
// watcher and closes the channel. The channel is buffered; if the consumer falls
// behind, the oldest queued deliveries are dropped in favor of newer ones.
//
// The caller must invoke cancel to release the watcher; doing so is safe to
// repeat. buffer sets the channel capacity and is raised to a minimum of one so
// a delivery is never lost to a zero-length buffer.
func (d *Diagnostic) Watch(buffer int) (<-chan PublishedDiagnostics, func()) {
	return d.sink.watch(buffer)
}

// Collect opens docURI on the client and gathers its diagnostics using the
// requested model, returning a flattened report.
//
// mode selects the diagnostic model; an empty mode defaults to [ModePush]. wait
// bounds the push-model window and is ignored for pull mode; a non-positive wait
// defaults to [DefaultPushWait]. languageID is the LSP language identifier the
// document is opened with.
//
// Opening the document is what gives the server the content to analyze and, in
// push mode, prompts it to compute and publish diagnostics; a pull against an
// unopened document would fall back to the on-disk file, which may be stale
// relative to text.
func (d *Diagnostic) Collect(ctx context.Context, docURI uri.URI, languageID protocol.LanguageKind, text string, mode Mode, wait time.Duration) (*Report, error) {
	switch mode {
	case "", ModePush:
		if wait <= 0 {
			wait = DefaultPushWait
		}
		return d.collectPush(ctx, docURI, languageID, text, wait)
	case ModePull:
		return d.collectPull(ctx, docURI, languageID, text)
	default:
		return nil, fmt.Errorf("lsp: unknown mode %q: want %q or %q", mode, ModePush, ModePull)
	}
}

// collectPush opens the document and waits for the server to publish
// diagnostics for it, returning the first delivery that names the document or an
// empty report when the wait window elapses.
func (d *Diagnostic) collectPush(ctx context.Context, docURI uri.URI, languageID protocol.LanguageKind, text string, wait time.Duration) (*Report, error) {
	// Register the watcher before opening so a publish triggered by didOpen
	// cannot race ahead of the subscription.
	events, cancel := d.Watch(8)
	defer cancel()

	if err := d.client.open(ctx, docURI, languageID, text); err != nil {
		return nil, fmt.Errorf("didOpen: %w", err)
	}

	timeout := time.NewTimer(wait)
	defer timeout.Stop()

	for {
		select {
		case event := <-events:
			if event.URI != docURI {
				// A server may publish for related documents (headers, imports);
				// keep waiting for the opened file.
				continue
			}
			return toReport(docURI, ModePush, false, event.Diagnostics), nil

		case <-timeout.C:
			// No diagnostics within the window: report the document as clean
			// rather than failing, matching the push model's "absence means none".
			return toReport(docURI, ModePush, false, nil), nil

		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// collectPull opens the document, then requests its diagnostics with the
// pull-model `textDocument/diagnostic` request and returns the report.
func (d *Diagnostic) collectPull(ctx context.Context, docURI uri.URI, languageID protocol.LanguageKind, text string) (*Report, error) {
	if err := d.client.open(ctx, docURI, languageID, text); err != nil {
		return nil, fmt.Errorf("didOpen: %w", err)
	}

	report, err := d.Pull(ctx, docURI, "", "")
	if err != nil {
		return nil, fmt.Errorf("textDocument/diagnostic: %w", err)
	}

	unchanged := report.Kind == protocol.DocumentDiagnosticReportKindUnchanged
	return toReport(docURI, ModePull, unchanged, report.Items), nil
}

// pushSink is the inbound half of the diagnostics feature: the [protocol.Client]
// callback the downstream server pushes into. It records the most recent
// published diagnostics per document and fans every delivery out to registered
// watchers.
//
// It embeds [protocol.UnimplementedClient] so only the diagnostic-relevant
// callback, PublishDiagnostics, is overridden; every other server->client method
// answers with the standard "not implemented" error. A pushSink makes no
// client->server calls — the outbound diagnostic API lives on [Diagnostic] — so
// it needs no connection handle. A pushSink is safe for concurrent use.
type pushSink struct {
	protocol.UnimplementedClient

	mu sync.Mutex
	// store holds the most recent published diagnostics per document. A publish
	// replaces the document's entry wholesale, matching the LSP contract that a
	// publishDiagnostics notification carries the complete current set for a URI.
	store map[uri.URI][]protocol.Diagnostic
	// watchers receive a copy of every publish. Each is a buffered channel with
	// drop-oldest delivery so a slow consumer can never stall the connection's
	// read loop.
	watchers map[int]chan PublishedDiagnostics
	nextID   int
}

// compile-time check that *pushSink satisfies the LSP client contract.
var _ protocol.Client = (*pushSink)(nil)

// newPushSink returns a pushSink ready to receive published diagnostics. It is
// fully constructed on return: the sink makes no outbound calls, so it needs no
// connection wired in afterwards.
func newPushSink() *pushSink {
	return &pushSink{
		store:    make(map[uri.URI][]protocol.Diagnostic),
		watchers: make(map[int]chan PublishedDiagnostics),
	}
}

// PublishDiagnostics implements [protocol.Client]. It records the published
// diagnostics for the document and fans the delivery out to every watcher.
//
// This callback runs on the connection's dispatch path, so it must not block.
// The watcher sends are non-blocking with drop-oldest semantics, and no
// client->server call is made here, so the read loop stays live.
func (s *pushSink) PublishDiagnostics(ctx context.Context, params *protocol.PublishDiagnosticsParams) error {
	// Copy the slice so a later mutation of params (the decoder may pool or reuse
	// backing storage) cannot corrupt the stored set.
	diags := slices.Clone(params.Diagnostics)

	s.mu.Lock()
	if len(diags) == 0 {
		// An empty set clears the document: the server is reporting that the
		// document now has no diagnostics, so retaining a stale entry would be
		// wrong.
		delete(s.store, params.URI)
	} else {
		s.store[params.URI] = diags
	}
	event := PublishedDiagnostics{
		URI:         params.URI,
		Version:     params.Version,
		Diagnostics: diags,
	}
	for _, ch := range s.watchers {
		deliver(ch, event)
	}
	s.mu.Unlock()

	return nil
}

// diagnostics returns a copy of the most recently published diagnostics for the
// given document. The boolean reports whether the server has published any
// diagnostics for the document; a published-then-cleared document reports false.
func (s *pushSink) diagnostics(docURI uri.URI) ([]protocol.Diagnostic, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	diags, ok := s.store[docURI]
	if !ok {
		return nil, false
	}

	return slices.Clone(diags), true
}

// watch registers a watcher that receives every subsequent push-model delivery.
// It returns the receive channel and a cancel function that unregisters the
// watcher and closes the channel. The channel is buffered; if the consumer falls
// behind, the oldest queued deliveries are dropped in favor of newer ones.
//
// The caller must invoke cancel to release the watcher; doing so is safe to
// repeat. buffer sets the channel capacity and is raised to a minimum of one so
// a delivery is never lost to a zero-length buffer.
func (s *pushSink) watch(buffer int) (<-chan PublishedDiagnostics, func()) {
	buffer = max(buffer, 1)
	ch := make(chan PublishedDiagnostics, buffer)

	s.mu.Lock()
	id := s.nextID
	s.nextID++
	s.watchers[id] = ch
	s.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if ch, ok := s.watchers[id]; ok {
				delete(s.watchers, id)
				close(ch)
			}
		})
	}

	return ch, cancel
}

// toReport flattens an LSP diagnostic set into a [Report], converting zero-based
// LSP positions to the one-based positions editors and humans use and resolving
// the union-typed severity, source, and message fields.
func toReport(docURI uri.URI, mode Mode, unchanged bool, diags []protocol.Diagnostic) *Report {
	out := &Report{
		URI:         docURI,
		Mode:        mode,
		Unchanged:   unchanged,
		Diagnostics: make([]FlatDiagnostic, 0, len(diags)),
	}
	for _, d := range diags {
		out.Diagnostics = append(out.Diagnostics, FlatDiagnostic{
			Line:      d.Range.Start.Line + 1,
			Column:    d.Range.Start.Character + 1,
			EndLine:   d.Range.End.Line + 1,
			EndColumn: d.Range.End.Character + 1,
			Severity:  severityName(d.Severity),
			Source:    diagnosticSource(d),
			Message:   diagnosticMessage(d),
		})
	}
	return out
}

// deliver sends event to ch without blocking. If the buffer is full it drops the
// oldest queued event to make room, so the newest diagnostics always win and a
// slow consumer can never wedge the producer.
func deliver(ch chan PublishedDiagnostics, event PublishedDiagnostics) {
	for {
		select {
		case ch <- event:
			return
		default:
			// Buffer full: drop the oldest queued event and retry. The drain is
			// itself non-blocking, so a concurrent consumer that empties the channel
			// between the failed send and this receive simply makes the retry succeed.
			select {
			case <-ch:
			default:
			}
		}
	}
}

// flattenRawReport interprets a raw document diagnostic report into a
// [DiagnosticReport]. A JSON null (the server reporting no result) yields an
// empty full report. An explicit "unchanged" kind yields an unchanged report.
// Every other shape — including the empty kind that gopls emits — is treated as
// a full report carrying whatever items were present.
func flattenRawReport(raw jsonrpc2.RawMessage) (*DiagnosticReport, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return &DiagnosticReport{Kind: protocol.DocumentDiagnosticReportKindFull}, nil
	}

	var rep rawDiagnosticReport
	if err := json.Unmarshal(raw, &rep); err != nil {
		return nil, fmt.Errorf("lsp: decoding diagnostic report: %w", err)
	}

	if rep.Kind == string(protocol.DocumentDiagnosticReportKindUnchanged) {
		return &DiagnosticReport{
			Kind:     protocol.DocumentDiagnosticReportKindUnchanged,
			ResultID: rep.ResultID,
		}, nil
	}

	return &DiagnosticReport{
		Kind:     protocol.DocumentDiagnosticReportKindFull,
		Items:    rep.Items,
		ResultID: rep.ResultID,
	}, nil
}

// severityName maps an LSP diagnostic severity to its conventional label,
// defaulting to "unknown" for the unset or out-of-range value.
func severityName(sev protocol.DiagnosticSeverity) string {
	switch sev {
	case protocol.DiagnosticSeverityError:
		return "error"
	case protocol.DiagnosticSeverityWarning:
		return "warning"
	case protocol.DiagnosticSeverityInformation:
		return "info"
	case protocol.DiagnosticSeverityHint:
		return "hint"
	default:
		return "unknown"
	}
}

// diagnosticSource returns the diagnostic's source string, or "" when the
// server did not provide one.
func diagnosticSource(d protocol.Diagnostic) string {
	if src, ok := d.Source.Get(); ok {
		return src
	}
	return ""
}

// diagnosticMessage extracts the human-readable message from a diagnostic. The
// Message field is a sealed union whose common arm is a plain string; a
// non-string arm (markup) is rendered through its Go representation so no
// information is silently dropped.
func diagnosticMessage(d protocol.Diagnostic) string {
	if s, ok := d.Message.(protocol.String); ok {
		return string(s)
	}
	return fmt.Sprintf("%v", d.Message)
}
