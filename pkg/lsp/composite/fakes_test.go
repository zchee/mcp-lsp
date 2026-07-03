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

package composite

import (
	"context"
	"fmt"

	"go.lsp.dev/protocol"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

// unsupported returns an error wrapping lsp.ErrUnsupported, as a gated pkg/lsp
// primitive would when its capability is unadvertised.
func unsupported(name string) error {
	return fmt.Errorf("%s request: %w", name, lsp.ErrUnsupported)
}

// fakeNavLooker satisfies navigationLooker (definition, declaration, type
// definition, implementation). It records its call count so tests can assert a
// leg was or was not invoked.
type fakeNavLooker struct {
	locs  []lsp.NavigationLocation
	err   error
	calls int
}

func (f *fakeNavLooker) Lookup(context.Context, string, string, string, protocol.Position) ([]lsp.NavigationLocation, error) {
	f.calls++
	return f.locs, f.err
}

// blockingNavLooker satisfies navigationLooker but blocks until the context is
// canceled, then returns the context error, so a test can drive the fan-out
// deadline path.
type blockingNavLooker struct{}

func (blockingNavLooker) Lookup(ctx context.Context, _, _, _ string, _ protocol.Position) ([]lsp.NavigationLocation, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

type fakeDocumentSymbolLooker struct {
	entries []lsp.DocumentSymbolEntry
	err     error
}

func (f *fakeDocumentSymbolLooker) Lookup(context.Context, string, string, string) ([]lsp.DocumentSymbolEntry, error) {
	return f.entries, f.err
}

type fakeHoverLooker struct {
	result *lsp.HoverResult
	err    error
	calls  int
}

func (f *fakeHoverLooker) Lookup(context.Context, string, string, string, protocol.Position) (*lsp.HoverResult, error) {
	f.calls++
	return f.result, f.err
}

type fakeSignatureHelpLooker struct {
	result *lsp.SignatureHelpResult
	err    error
}

func (f *fakeSignatureHelpLooker) Lookup(context.Context, string, string, string, protocol.Position) (*lsp.SignatureHelpResult, error) {
	return f.result, f.err
}

type fakeDocumentHighlightLooker struct {
	spans []lsp.DocumentHighlightSpan
	err   error
}

func (f *fakeDocumentHighlightLooker) Lookup(context.Context, string, string, string, protocol.Position) ([]lsp.DocumentHighlightSpan, error) {
	return f.spans, f.err
}

type fakeInlayHintLooker struct {
	items []lsp.InlayHintItem
	err   error
}

func (f *fakeInlayHintLooker) Lookup(context.Context, string, string, string, protocol.Range) ([]lsp.InlayHintItem, error) {
	return f.items, f.err
}

type fakeCapabilityProbe struct {
	snap lsp.CapabilitySnapshot
	err  error
}

func (f fakeCapabilityProbe) CapabilitySnapshot(context.Context, string) (lsp.CapabilitySnapshot, error) {
	return f.snap, f.err
}

// fakeReferencesLooker satisfies referencesLooker. If seq is set, each call
// returns the next slice in seq, so a test can drive readiness instability.
type fakeReferencesLooker struct {
	locs  []lsp.NavigationLocation
	seq   [][]lsp.NavigationLocation
	err   error
	calls int
}

func (f *fakeReferencesLooker) Lookup(_ context.Context, _, _, _ string, _ protocol.Position, _ bool) ([]lsp.NavigationLocation, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if len(f.seq) > 0 {
		i := min(f.calls-1, len(f.seq)-1)
		return f.seq[i], nil
	}
	return f.locs, f.err
}

// fakeCallHierarchyLooker satisfies callHierarchyLooker with canned prepare,
// incoming, and outgoing results.
type fakeCallHierarchyLooker struct {
	prepare  []protocol.CallHierarchyItem
	prepErr  error
	incoming []protocol.CallHierarchyIncomingCall
	outgoing []protocol.CallHierarchyOutgoingCall
	calls    int
}

func (f *fakeCallHierarchyLooker) Prepare(context.Context, string, string, string, protocol.Position) ([]protocol.CallHierarchyItem, error) {
	f.calls++
	return f.prepare, f.prepErr
}

func (f *fakeCallHierarchyLooker) IncomingCalls(context.Context, string, *protocol.CallHierarchyItem) ([]protocol.CallHierarchyIncomingCall, error) {
	return f.incoming, nil
}

func (f *fakeCallHierarchyLooker) OutgoingCalls(context.Context, string, *protocol.CallHierarchyItem) ([]protocol.CallHierarchyOutgoingCall, error) {
	return f.outgoing, nil
}

// fakeTypeHierarchyLooker satisfies typeHierarchyLooker.
type fakeTypeHierarchyLooker struct {
	prepare []protocol.TypeHierarchyItem
	prepErr error
	supers  []protocol.TypeHierarchyItem
	subs    []protocol.TypeHierarchyItem
}

func (f *fakeTypeHierarchyLooker) Prepare(context.Context, string, string, string, protocol.Position) ([]protocol.TypeHierarchyItem, error) {
	return f.prepare, f.prepErr
}

func (f *fakeTypeHierarchyLooker) Supertypes(context.Context, string, *protocol.TypeHierarchyItem) ([]protocol.TypeHierarchyItem, error) {
	return f.supers, nil
}

func (f *fakeTypeHierarchyLooker) Subtypes(context.Context, string, *protocol.TypeHierarchyItem) ([]protocol.TypeHierarchyItem, error) {
	return f.subs, nil
}

// fakeDiagnosticsLooker satisfies the diagnostics facade's looker and the
// change_guard epicenter looker. If seq is set, each call returns the next slice
// so a test can drive pull-path readiness instability.
type fakeDiagnosticsLooker struct {
	diags []lsp.Diagnostic
	seq   [][]lsp.Diagnostic
	err   error
	calls int
}

func (f *fakeDiagnosticsLooker) Lookup(context.Context, string, string, string) ([]lsp.Diagnostic, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if len(f.seq) > 0 {
		i := min(f.calls-1, len(f.seq)-1)
		return f.seq[i], nil
	}
	return f.diags, nil
}

// fakeCodeActionLooker satisfies codeActionLooker.
type fakeCodeActionLooker struct {
	actions []lsp.CodeAction
	err     error
}

func (f *fakeCodeActionLooker) Lookup(context.Context, string, string, string, protocol.Range, []protocol.CodeActionKind, bool) ([]lsp.CodeAction, error) {
	return f.actions, f.err
}
