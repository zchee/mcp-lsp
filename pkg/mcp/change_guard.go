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
	"go.lsp.dev/uri"

	"github.com/zchee/mcp-lsp/pkg/lsp"
	"github.com/zchee/mcp-lsp/pkg/lsp/composite"
)

// changeGuardAnalyzer is the narrow dependency the lsp_change_guard handler
// needs.
type changeGuardAnalyzer interface {
	Analyze(ctx context.Context, lang, absPath, text string, baseline []lsp.DocumentSymbolEntry) (composite.ChangeGuardResult, error)
}

// ChangeGuardInput is the input schema for lsp_change_guard. The file must be
// the post-edit, on-disk content the agent has already written.
type ChangeGuardInput struct {
	File     string `json:"file"               jsonschema:"absolute or workspace-relative path to the changed file, already written to disk"`
	Language string `json:"language,omitempty" jsonschema:"language id of the file; inferred from file when omitted"`
}

// ChangeGuardDiagnosticItem is one diagnostic in the report, with a one-based
// range.
type ChangeGuardDiagnosticItem struct {
	Range    DefinitionRangeItem `json:"range"`
	Severity string              `json:"severity"`
	Source   string              `json:"source,omitempty"`
	Code     string              `json:"code,omitempty"`
	Message  string              `json:"message"`
}

// ChangeGuardOutput is the output schema for lsp_change_guard. AdvisoryVerdict
// is empty when diagnostics were not ready (see Meta.readiness); it is never a
// clean verdict rendered from cold or unsettled data.
type ChangeGuardOutput struct {
	File               string                      `json:"file"`
	URI                string                      `json:"uri"`
	AdvisoryVerdict    string                      `json:"advisoryVerdict"`
	Basis              []string                    `json:"basis"`
	Hedge              string                      `json:"hedge"`
	Diagnostics        []ChangeGuardDiagnosticItem `json:"diagnostics"`
	DiagnosticsStatus  string                      `json:"diagnosticsStatus"`
	ChangedSymbols     []string                    `json:"changedSymbols,omitempty"`
	DisappearedSymbols []string                    `json:"disappearedSymbols,omitempty"`
	QuickFixes         []string                    `json:"quickFixes"`
	Meta               MetaOutput                  `json:"meta"`
}

func changeGuardHandler(analyzer changeGuardAnalyzer, workspaceRoot string, resolver languageResolver) mcp.ToolHandlerFor[ChangeGuardInput, ChangeGuardOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ChangeGuardInput) (*mcp.CallToolResult, ChangeGuardOutput, error) {
		absPath, text, lang, err := readInputFile(workspaceRoot, in.File, in.Language, resolver)
		if err != nil {
			return nil, ChangeGuardOutput{}, err
		}
		res, err := analyzer.Analyze(ctx, lang, absPath, text, nil)
		if err != nil {
			return nil, ChangeGuardOutput{}, err
		}
		return nil, ChangeGuardOutput{
			File:               absPath,
			URI:                string(uri.File(absPath)),
			AdvisoryVerdict:    string(res.AdvisoryVerdict),
			Basis:              res.Basis,
			Hedge:              res.Hedge,
			Diagnostics:        changeGuardDiagnostics(res.Diagnostics.Data),
			DiagnosticsStatus:  res.Diagnostics.Status.String(),
			ChangedSymbols:     res.ChangedSymbols,
			DisappearedSymbols: res.DisappearedSymbols,
			QuickFixes:         quickFixTitles(res.QuickFixes.Data),
			Meta:               metaOutput(&res.Meta),
		}, nil
	}
}

func changeGuardDiagnostics(diags []lsp.Diagnostic) []ChangeGuardDiagnosticItem {
	items := make([]ChangeGuardDiagnosticItem, 0, len(diags))
	for i := range diags {
		items = append(items, ChangeGuardDiagnosticItem{
			Range: toNavigationRangeItem(lsp.NavigationRange{
				StartLine:   diags[i].StartLine,
				StartColumn: diags[i].StartColumn,
				EndLine:     diags[i].EndLine,
				EndColumn:   diags[i].EndColumn,
			}),
			Severity: diags[i].Severity,
			Source:   diags[i].Source,
			Code:     diags[i].Code,
			Message:  diags[i].Message,
		})
	}
	return items
}

func quickFixTitles(actions []lsp.CodeAction) []string {
	titles := make([]string, 0, len(actions))
	for i := range actions {
		titles = append(titles, actions[i].Title)
	}
	return titles
}
