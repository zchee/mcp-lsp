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
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

// refLooker is the narrow dependency the find-references handler needs from
// the LSP layer. It lets tests substitute a fake without spawning a language
// server.
type refLooker interface {
	Lookup(ctx context.Context, lang, absPath, text string, pos protocol.Position, includeDeclaration bool) ([]lsp.NavigationLocation, error)
}

// ReferencesInput is the input schema for the lsp_find_references tool. Line
// and column are one-based for the agent.
type ReferencesInput struct {
	File               string `json:"file"                         jsonschema:"absolute or workspace-relative path to the file to query"`
	Line               int    `json:"line"                         jsonschema:"one-based line containing the symbol reference"`
	Column             int    `json:"column"                       jsonschema:"one-based column containing the symbol reference"`
	Language           string `json:"language,omitempty"           jsonschema:"language id of the file; inferred from file when omitted"`
	IncludeDeclaration bool   `json:"includeDeclaration,omitempty" jsonschema:"include the symbol's declaration among the results"`
}

// ReferenceItem is one reference location returned by the lsp_find_references
// tool, with a one-based range.
type ReferenceItem struct {
	URI   string              `json:"uri"`
	Range DefinitionRangeItem `json:"range"`
}

// ReferencesOutput is the output schema for the lsp_find_references tool.
// Readiness reports how the readiness gate concluded: "stable" means two
// consecutive lookups agreed and the result can be trusted, "exhausted" means
// the language server never produced two agreeing results (it is likely still
// indexing) and an empty result must NOT be read as "no references".
type ReferencesOutput struct {
	File       string          `json:"file"`
	URI        string          `json:"uri"`
	References []ReferenceItem `json:"references"`
	Readiness  string          `json:"readiness"`
}

// referencesHandler returns the tool handler bound to looker. The handler
// validates input, reads the file, and runs the lookup under the
// retry-until-stable readiness gate so a cold-indexing server never yields a
// confidently-empty reference list. Positions convert between one-based agent
// coordinates and zero-based protocol coordinates at this boundary.
func referencesHandler(looker refLooker, workspaceRoot string, resolver languageResolver) mcp.ToolHandlerFor[ReferencesInput, ReferencesOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ReferencesInput) (*mcp.CallToolResult, ReferencesOutput, error) {
		pos, err := navigationInputPosition(in.Line, in.Column)
		if err != nil {
			return nil, ReferencesOutput{}, err
		}
		absPath, text, lang, err := readInputFile(workspaceRoot, in.File, in.Language, resolver)
		if err != nil {
			return nil, ReferencesOutput{}, err
		}
		refs, readiness, err := lsp.Stable(ctx, lsp.StableConfig{}, canonicalNavigationLocations, func(ctx context.Context) ([]lsp.NavigationLocation, error) {
			return looker.Lookup(ctx, lang, absPath, text, pos, in.IncludeDeclaration)
		})
		if err != nil {
			return nil, ReferencesOutput{}, err
		}
		return nil, ReferencesOutput{
			File:       absPath,
			URI:        string(uri.File(absPath)),
			References: toReferenceItems(refs),
			Readiness:  readiness.String(),
		}, nil
	}
}

// canonicalNavigationLocations sorts locations by target URI and range so
// stability comparison and tool output are deterministic even when the server
// reorders results between lookups.
func canonicalNavigationLocations(in []lsp.NavigationLocation) []lsp.NavigationLocation {
	slices.SortFunc(in, func(a, b lsp.NavigationLocation) int {
		if c := strings.Compare(a.TargetURI, b.TargetURI); c != 0 {
			return c
		}
		if c := cmp.Compare(a.TargetRange.StartLine, b.TargetRange.StartLine); c != 0 {
			return c
		}
		if c := cmp.Compare(a.TargetRange.StartColumn, b.TargetRange.StartColumn); c != 0 {
			return c
		}
		if c := cmp.Compare(a.TargetRange.EndLine, b.TargetRange.EndLine); c != 0 {
			return c
		}
		return cmp.Compare(a.TargetRange.EndColumn, b.TargetRange.EndColumn)
	})
	return in
}

// toReferenceItems converts zero-based [lsp.NavigationLocation] values into
// one-based tool items.
func toReferenceItems(refs []lsp.NavigationLocation) []ReferenceItem {
	items := make([]ReferenceItem, 0, len(refs))
	for _, ref := range refs {
		items = append(items, ReferenceItem{
			URI:   ref.TargetURI,
			Range: toNavigationRangeItem(ref.TargetRange),
		})
	}
	return items
}
