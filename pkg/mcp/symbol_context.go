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

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/zchee/mcp-lsp/pkg/lsp"
	"github.com/zchee/mcp-lsp/pkg/lsp/composite"
)

// symbolContextAnalyzer is the narrow dependency the lsp_symbol_context handler
// needs, letting tests substitute a fake composite.
type symbolContextAnalyzer interface {
	Analyze(ctx context.Context, lang, absPath, text string, pos protocol.Position) (composite.SymbolContextResult, error)
}

// SymbolContextInput is the input schema for lsp_symbol_context. Line and column
// are one-based for the agent.
type SymbolContextInput struct {
	File     string `json:"file"               jsonschema:"absolute or workspace-relative path to the file to query"`
	Line     int    `json:"line"               jsonschema:"one-based line containing the symbol"`
	Column   int    `json:"column"             jsonschema:"one-based column containing the symbol"`
	Language string `json:"language,omitempty" jsonschema:"language id of the file; inferred from file when omitted"`
}

// MetaOutput carries a composite's shared metadata, converted for the agent.
type MetaOutput struct {
	Readiness           string   `json:"readiness"`
	StopReason          string   `json:"stopReason"`
	EpicenterTextHash   string   `json:"epicenterTextHash"`
	CapabilitiesUsed    []string `json:"capabilitiesUsed"`
	CapabilitiesMissing []string `json:"capabilitiesMissing"`
}

// SymbolContextOutput is the output schema for lsp_symbol_context. Optional legs
// carry a per-leg status so an agent can tell a capability gap from an empty
// result. Positions are one-based.
type SymbolContextOutput struct {
	File               string     `json:"file"`
	URI                string     `json:"uri"`
	Hover              *HoverItem `json:"hover,omitempty"`
	EnclosingSymbols   LegOutput  `json:"enclosingSymbols"`
	Signature          LegOutput  `json:"signature"`
	Definitions        LegOutput  `json:"definitions"`
	Declaration        LegOutput  `json:"declaration"`
	TypeDefinition     LegOutput  `json:"typeDefinition"`
	SameFileHighlights LegOutput  `json:"sameFileHighlights"`
	InlayHints         LegOutput  `json:"inlayHints"`
	Meta               MetaOutput `json:"meta"`
}

// LegOutput is the agent-facing shape of a composite leg: its status and a
// human-readable note explaining any non-ok status. The concrete data of each
// leg is summarized into count and, where useful, locations, keeping the schema
// uniform across legs.
type LegOutput struct {
	Status    string                `json:"status"`
	Note      string                `json:"note,omitempty"`
	Count     int                   `json:"count"`
	Locations []DefinitionRangeItem `json:"locations,omitempty"`
}

func symbolContextHandler(analyzer symbolContextAnalyzer, workspaceRoot string, resolver languageResolver) mcp.ToolHandlerFor[SymbolContextInput, SymbolContextOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in SymbolContextInput) (*mcp.CallToolResult, SymbolContextOutput, error) {
		pos, err := navigationInputPosition(in.Line, in.Column)
		if err != nil {
			return nil, SymbolContextOutput{}, err
		}
		absPath, text, lang, err := readInputFile(workspaceRoot, in.File, in.Language, resolver)
		if err != nil {
			return nil, SymbolContextOutput{}, err
		}
		res, err := analyzer.Analyze(ctx, lang, absPath, text, pos)
		if err != nil {
			return nil, SymbolContextOutput{}, err
		}
		return nil, SymbolContextOutput{
			File:               absPath,
			URI:                string(uri.File(absPath)),
			Hover:              hoverItemFromResult(res.Hover),
			EnclosingSymbols:   locationLegFromSymbols(res.EnclosingSymbols),
			Signature:          statusOnlyLeg(res.Signature.Status.String(), res.Signature.Note, signatureCount(res.Signature.Data)),
			Definitions:        locationLeg(res.Definitions),
			Declaration:        locationLeg(res.Declaration),
			TypeDefinition:     locationLeg(res.TypeDefinition),
			SameFileHighlights: highlightLeg(res.SameFileHighlights),
			InlayHints:         statusOnlyLeg(res.InlayHints.Status.String(), res.InlayHints.Note, len(res.InlayHints.Data)),
			Meta:               metaOutput(&res.Meta),
		}, nil
	}
}

func hoverItemFromResult(h *lsp.HoverResult) *HoverItem {
	if h == nil {
		return nil
	}
	item := &HoverItem{Kind: h.Kind, Value: h.Value}
	if h.Range != nil {
		r := toNavigationRangeItem(*h.Range)
		item.Range = &r
	}
	return item
}

func locationLeg(leg composite.Leg[[]lsp.NavigationLocation]) LegOutput {
	out := LegOutput{Status: leg.Status.String(), Note: leg.Note, Count: len(leg.Data)}
	for i := range leg.Data {
		out.Locations = append(out.Locations, toNavigationRangeItem(leg.Data[i].TargetRange))
	}
	return out
}

func locationLegFromSymbols(leg composite.Leg[[]lsp.DocumentSymbolEntry]) LegOutput {
	out := LegOutput{Status: leg.Status.String(), Note: leg.Note, Count: len(leg.Data)}
	for i := range leg.Data {
		out.Locations = append(out.Locations, toNavigationRangeItem(leg.Data[i].SelectionRange))
	}
	return out
}

func highlightLeg(leg composite.Leg[[]lsp.DocumentHighlightSpan]) LegOutput {
	out := LegOutput{Status: leg.Status.String(), Note: leg.Note, Count: len(leg.Data)}
	for i := range leg.Data {
		out.Locations = append(out.Locations, toNavigationRangeItem(leg.Data[i].Range))
	}
	return out
}

func statusOnlyLeg(status, note string, count int) LegOutput {
	return LegOutput{Status: status, Note: note, Count: count}
}

func signatureCount(sig *lsp.SignatureHelpResult) int {
	if sig == nil {
		return 0
	}
	return len(sig.Signatures)
}

func metaOutput(m *composite.Meta) MetaOutput {
	return MetaOutput{
		Readiness:           m.Readiness,
		StopReason:          m.StopReason,
		EpicenterTextHash:   m.EpicenterTextHash,
		CapabilitiesUsed:    capabilityStrings(m.CapabilitiesUsed),
		CapabilitiesMissing: capabilityStrings(m.CapabilitiesMissing),
	}
}

func capabilityStrings(caps []composite.Capability) []string {
	out := make([]string, 0, len(caps))
	for _, c := range caps {
		out = append(out, string(c))
	}
	return out
}
