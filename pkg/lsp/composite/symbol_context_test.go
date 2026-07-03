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
	"errors"
	"testing"

	"go.lsp.dev/protocol"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

// symbolContextEngine builds an Engine wired entirely from fakes, so the
// composite can be exercised without a language server.
func symbolContextEngine(snap lsp.CapabilitySnapshot, hover *fakeHoverLooker, secondary secondaryFakes) *Engine {
	return &Engine{
		hover:             hover,
		documentSymbol:    secondary.docSym,
		signatureHelp:     secondary.sig,
		definition:        secondary.def,
		declaration:       secondary.decl,
		typeDefinition:    secondary.typeDef,
		documentHighlight: secondary.highlight,
		inlayHint:         secondary.inlay,
		capabilities:      fakeCapabilityProbe{snap: snap},
	}
}

type secondaryFakes struct {
	docSym    *fakeDocumentSymbolLooker
	sig       *fakeSignatureHelpLooker
	def       *fakeNavLooker
	decl      *fakeNavLooker
	typeDef   *fakeNavLooker
	highlight *fakeDocumentHighlightLooker
	inlay     *fakeInlayHintLooker
}

func allOkSecondary() secondaryFakes {
	return secondaryFakes{
		docSym:    &fakeDocumentSymbolLooker{entries: []lsp.DocumentSymbolEntry{{Name: "Manager", Kind: protocol.SymbolKindStruct}}},
		sig:       &fakeSignatureHelpLooker{result: &lsp.SignatureHelpResult{Signatures: []lsp.SignatureInfo{{Label: "f()"}}}},
		def:       &fakeNavLooker{locs: []lsp.NavigationLocation{{TargetURI: "file:///def.go"}}},
		decl:      &fakeNavLooker{locs: []lsp.NavigationLocation{{TargetURI: "file:///decl.go"}}},
		typeDef:   &fakeNavLooker{locs: []lsp.NavigationLocation{{TargetURI: "file:///type.go"}}},
		highlight: &fakeDocumentHighlightLooker{spans: []lsp.DocumentHighlightSpan{{Kind: "write"}}},
		inlay:     &fakeInlayHintLooker{items: []lsp.InlayHintItem{{Label: "int"}}},
	}
}

func fullSnapshot() lsp.CapabilitySnapshot {
	return lsp.CapabilitySnapshot{
		Hover: true, DocumentSymbol: true, SignatureHelp: true, Declaration: true,
		TypeDefinition: true, DocumentHighlight: true, InlayHint: true,
	}
}

func TestSymbolContextAnalyzeAllLegsOK(t *testing.T) {
	t.Parallel()

	hover := &fakeHoverLooker{result: &lsp.HoverResult{Kind: "markdown", Value: "doc"}}
	engine := symbolContextEngine(fullSnapshot(), hover, allOkSecondary())

	got, err := NewSymbolContext(engine).Analyze(t.Context(), "go", "/ws/main.go", "package main\n", protocol.Position{Line: 5})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.Hover == nil || got.Hover.Value != "doc" {
		t.Errorf("Hover = %+v, want the fake hover", got.Hover)
	}
	for name, status := range map[string]LegStatus{
		"enclosing":  got.EnclosingSymbols.Status,
		"signature":  got.Signature.Status,
		"definition": got.Definitions.Status,
		"highlights": got.SameFileHighlights.Status,
		"inlay":      got.InlayHints.Status,
	} {
		if status != StatusOK {
			t.Errorf("%s leg status = %v, want ok", name, status)
		}
	}
	if got.Meta.EpicenterTextHash == "" {
		t.Error("Meta.EpicenterTextHash is empty, want the sha256 of the epicenter text")
	}
	if got.Meta.Readiness != "stable" {
		t.Errorf("Meta.Readiness = %q, want stable", got.Meta.Readiness)
	}
}

func TestSymbolContextAnalyzeEpicenterUnsupportedErrors(t *testing.T) {
	t.Parallel()

	hover := &fakeHoverLooker{err: unsupported("hover")}
	secondary := allOkSecondary()
	engine := symbolContextEngine(lsp.CapabilitySnapshot{}, hover, secondary)

	_, err := NewSymbolContext(engine).Analyze(t.Context(), "go", "/ws/main.go", "package main\n", protocol.Position{})
	if !errors.Is(err, lsp.ErrUnsupported) {
		t.Fatalf("Analyze error = %v, want errors.Is lsp.ErrUnsupported (Hover is the epicenter)", err)
	}
	if secondary.def.calls != 0 {
		t.Errorf("definition leg was called %d times after epicenter failure, want 0", secondary.def.calls)
	}
}

func TestSymbolContextAnalyzeSecondaryDegrades(t *testing.T) {
	t.Parallel()

	hover := &fakeHoverLooker{result: &lsp.HoverResult{Value: "doc"}}
	secondary := allOkSecondary()
	// Declaration unsupported (gopls-like), signature help off a call site
	// (nil result), the rest ok.
	secondary.decl = &fakeNavLooker{err: unsupported("declaration")}
	secondary.sig = &fakeSignatureHelpLooker{result: nil}
	snap := fullSnapshot()
	snap.Declaration = false
	engine := symbolContextEngine(snap, hover, secondary)

	got, err := NewSymbolContext(engine).Analyze(t.Context(), "go", "/ws/main.go", "package main\n", protocol.Position{})
	if err != nil {
		t.Fatalf("Analyze must not error when only secondary legs degrade: %v", err)
	}
	if got.Declaration.Status != StatusUnsupported {
		t.Errorf("Declaration leg = %v, want unsupported (distinct from empty)", got.Declaration.Status)
	}
	if got.Signature.Status != StatusEmpty {
		t.Errorf("Signature leg = %v, want empty (off a call site)", got.Signature.Status)
	}
	if got.Definitions.Status != StatusOK {
		t.Errorf("Definitions leg = %v, want ok (unaffected by other legs)", got.Definitions.Status)
	}
	if !contains(got.Meta.CapabilitiesMissing, CapDeclaration) {
		t.Errorf("CapabilitiesMissing = %v, want it to include declaration", got.Meta.CapabilitiesMissing)
	}
}

func TestSymbolContextAnalyzeHoverEmpty(t *testing.T) {
	t.Parallel()

	hover := &fakeHoverLooker{result: nil}
	engine := symbolContextEngine(fullSnapshot(), hover, allOkSecondary())

	got, err := NewSymbolContext(engine).Analyze(t.Context(), "go", "/ws/main.go", "package main\n", protocol.Position{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.Hover != nil {
		t.Errorf("Hover = %+v, want nil for an empty hover", got.Hover)
	}
	if got.Meta.Readiness != "stable" {
		t.Errorf("Meta.Readiness = %q, want stable (an empty hover is still a resolved epicenter)", got.Meta.Readiness)
	}
}

func contains(caps []Capability, want Capability) bool {
	for _, c := range caps {
		if c == want {
			return true
		}
	}
	return false
}
