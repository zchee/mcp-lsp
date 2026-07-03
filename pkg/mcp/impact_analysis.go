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

	"github.com/zchee/mcp-lsp/pkg/lsp/composite"
)

// impactAnalyzer is the narrow dependency the lsp_impact_analysis handler needs.
type impactAnalyzer interface {
	Analyze(ctx context.Context, lang, absPath, text string, pos protocol.Position) (composite.ImpactResult, error)
}

// ImpactAnalysisInput is the input schema for lsp_impact_analysis. Line and
// column are one-based for the agent.
type ImpactAnalysisInput struct {
	File     string `json:"file"               jsonschema:"absolute or workspace-relative path to the file containing the symbol"`
	Line     int    `json:"line"               jsonschema:"one-based line containing the symbol to analyze"`
	Column   int    `json:"column"             jsonschema:"one-based column containing the symbol to analyze"`
	Language string `json:"language,omitempty" jsonschema:"language id of the file; inferred from file when omitted"`
}

// ImpactAnalysisOutput is the output schema for lsp_impact_analysis. Each leg
// carries a per-leg status; positions are one-based.
type ImpactAnalysisOutput struct {
	File            string     `json:"file"`
	URI             string     `json:"uri"`
	References      LegOutput  `json:"references"`
	CallGraph       LegOutput  `json:"callGraph"`
	TypeGraph       LegOutput  `json:"typeGraph"`
	Implementations LegOutput  `json:"implementations"`
	Definitions     LegOutput  `json:"definitions"`
	Declaration     LegOutput  `json:"declaration"`
	TypeDefinition  LegOutput  `json:"typeDefinition"`
	Diagnostics     LegOutput  `json:"diagnostics"`
	AffectedFiles   []string   `json:"affectedFiles"`
	Meta            MetaOutput `json:"meta"`
}

func impactAnalysisHandler(analyzer impactAnalyzer, workspaceRoot string, resolver languageResolver) mcp.ToolHandlerFor[ImpactAnalysisInput, ImpactAnalysisOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ImpactAnalysisInput) (*mcp.CallToolResult, ImpactAnalysisOutput, error) {
		pos, err := navigationInputPosition(in.Line, in.Column)
		if err != nil {
			return nil, ImpactAnalysisOutput{}, err
		}
		absPath, text, lang, err := readInputFile(workspaceRoot, in.File, in.Language, resolver)
		if err != nil {
			return nil, ImpactAnalysisOutput{}, err
		}
		res, err := analyzer.Analyze(ctx, lang, absPath, text, pos)
		if err != nil {
			return nil, ImpactAnalysisOutput{}, err
		}
		return nil, ImpactAnalysisOutput{
			File:            absPath,
			URI:             string(uri.File(absPath)),
			References:      refLeg(res.References),
			CallGraph:       statusOnlyLeg(res.CallGraph.Status.String(), res.CallGraph.Note, len(res.CallGraph.Data)),
			TypeGraph:       refLeg(res.TypeGraph),
			Implementations: locationLeg(res.Implementations),
			Definitions:     locationLeg(res.Definitions),
			Declaration:     locationLeg(res.Declaration),
			TypeDefinition:  locationLeg(res.TypeDefinition),
			Diagnostics:     statusOnlyLeg(res.Diagnostics.Status.String(), res.Diagnostics.Note, len(res.Diagnostics.Data)),
			AffectedFiles:   res.AffectedFiles,
			Meta:            metaOutput(&res.Meta),
		}, nil
	}
}

// refLeg converts a leg of composite refs into the uniform LegOutput, mapping
// each ref's zero-based range to a one-based location.
func refLeg(leg composite.Leg[[]composite.Ref]) LegOutput {
	out := LegOutput{Status: leg.Status.String(), Note: leg.Note, Count: len(leg.Data)}
	for i := range leg.Data {
		out.Locations = append(out.Locations, toNavigationRangeItem(leg.Data[i].Range))
	}
	return out
}
