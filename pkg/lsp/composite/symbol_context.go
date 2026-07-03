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

	"go.lsp.dev/protocol"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

// SymbolContext assembles a dense, understand-before-change card for a symbol:
// its hover, enclosing outline, signature, navigation targets, same-file
// occurrences, and inlay context, gathered in one call. Hover is the epicenter
// leg; every other leg degrades independently so a missing capability yields a
// partial card rather than an error.
type SymbolContext struct {
	engine *Engine
	budget Budget
}

// NewSymbolContext returns a symbol-context composite backed by engine.
func NewSymbolContext(engine *Engine) *SymbolContext {
	return &SymbolContext{engine: engine, budget: DefaultBudget()}
}

// SymbolContextResult is the assembled card with zero-based positions. Every
// optional leg is a [Leg] carrying its own status; Meta reports the overall
// readiness and which capabilities were available.
type SymbolContextResult struct {
	Hover              *lsp.HoverResult
	EnclosingSymbols   Leg[[]lsp.DocumentSymbolEntry]
	Signature          Leg[*lsp.SignatureHelpResult]
	Definitions        Leg[[]lsp.NavigationLocation]
	Declaration        Leg[[]lsp.NavigationLocation]
	TypeDefinition     Leg[[]lsp.NavigationLocation]
	SameFileHighlights Leg[[]lsp.DocumentHighlightSpan]
	InlayHints         Leg[[]lsp.InlayHintItem]
	Meta               Meta
}

// symbolContextCapabilities are the capabilities the card requests, reported as
// used/missing in the result Meta.
var symbolContextCapabilities = []Capability{
	CapHover, CapDocumentSymbol, CapDefinition, CapDeclaration,
	CapTypeDefinition, CapDocumentHighlight,
}

// Analyze builds the card for the symbol at pos in absPath. The caller supplies
// text (read once from disk); it is threaded unchanged through every leg so the
// language server answers each request against the same document snapshot. If
// the epicenter Hover capability is unsupported, Analyze returns an error
// wrapping [lsp.ErrUnsupported]; every other unsupported leg degrades to
// [StatusUnsupported].
func (s *SymbolContext) Analyze(ctx context.Context, lang, absPath, text string, pos protocol.Position) (SymbolContextResult, error) {
	report, err := Report(ctx, s.engine.capabilities, lang, symbolContextCapabilities)
	if err != nil {
		return SymbolContextResult{}, err
	}

	hover, err := s.engine.hover.Lookup(ctx, lang, absPath, text, pos)
	if err != nil {
		// Hover is the epicenter: an unsupported or failed epicenter has no
		// meaningful card to return.
		return SymbolContextResult{}, err
	}

	result := SymbolContextResult{
		Hover: hover,
		Meta: Meta{
			Readiness:           "stable",
			StopReason:          StopStable.String(),
			EpicenterTextHash:   hashText(text),
			CapabilitiesUsed:    report.Used,
			CapabilitiesMissing: report.Missing,
		},
	}

	enclosing, err := s.engine.documentSymbol.Lookup(ctx, lang, absPath, text)
	result.EnclosingSymbols = LegFrom(enclosing, err)

	sig, sigErr := s.engine.signatureHelp.Lookup(ctx, lang, absPath, text, pos)
	result.Signature = LegFromPointer(sig, sigErr)

	defs, defErr := s.engine.definition.Lookup(ctx, lang, absPath, text, pos)
	result.Definitions = LegFrom(defs, defErr)

	decls, declErr := s.engine.declaration.Lookup(ctx, lang, absPath, text, pos)
	result.Declaration = LegFrom(decls, declErr)

	typeDefs, typeErr := s.engine.typeDefinition.Lookup(ctx, lang, absPath, text, pos)
	result.TypeDefinition = LegFrom(typeDefs, typeErr)

	highlights, hlErr := s.engine.documentHighlight.Lookup(ctx, lang, absPath, text, pos)
	result.SameFileHighlights = LegFrom(highlights, hlErr)

	result.InlayHints = s.inlayHintsLeg(ctx, lang, absPath, text, pos)

	return result, nil
}

// inlayHintsLeg gathers inlay hints for a small range around pos, since inlay
// hints are range-scoped rather than position-scoped.
func (s *SymbolContext) inlayHintsLeg(ctx context.Context, lang, absPath, text string, pos protocol.Position) Leg[[]lsp.InlayHintItem] {
	rng := protocol.Range{Start: pos, End: protocol.Position{Line: pos.Line + 1, Character: 0}}
	hints, err := s.engine.inlayHint.Lookup(ctx, lang, absPath, text, rng)
	return LegFrom(hints, err)
}
