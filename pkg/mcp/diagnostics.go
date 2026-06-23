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

// Package mcp exposes language server capabilities as Model Context Protocol
// tools over stdio.
package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

// defaultLanguage is used when the tool input omits the language identifier.
const defaultLanguage = "go"

// diagLooker is the narrow dependency the diagnostics handler needs from the
// LSP layer. It lets tests substitute a fake without spawning a language server.
type diagLooker interface {
	Lookup(ctx context.Context, lang, absPath, text string) ([]lsp.Diagnostic, error)
}

// DiagnosticsInput is the input schema for the lsp_diagnostics tool.
type DiagnosticsInput struct {
	File     string `json:"file"               jsonschema:"absolute or workspace-relative path to the file to analyze"`
	Language string `json:"language,omitempty" jsonschema:"language id of the file; defaults to go"`
}

// DiagnosticItem is one diagnostic reported by the language server. Positions
// are one-based for the agent (line 1, column 1 is the first character).
type DiagnosticItem struct {
	Severity    string `json:"severity"`
	Message     string `json:"message"`
	Source      string `json:"source,omitempty"`
	Code        string `json:"code,omitempty"`
	StartLine   int    `json:"startLine"`
	StartColumn int    `json:"startColumn"`
	EndLine     int    `json:"endLine"`
	EndColumn   int    `json:"endColumn"`
}

// DiagnosticsOutput is the output schema for the lsp_diagnostics tool.
type DiagnosticsOutput struct {
	File        string           `json:"file"`
	Diagnostics []DiagnosticItem `json:"diagnostics"`
}

// diagnosticsHandler returns the tool handler bound to looker. The handler
// validates the input, reads the file, looks up diagnostics, and converts the
// zero-based LSP positions to one-based agent positions.
func diagnosticsHandler(looker diagLooker) mcp.ToolHandlerFor[DiagnosticsInput, DiagnosticsOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in DiagnosticsInput) (*mcp.CallToolResult, DiagnosticsOutput, error) {
		if in.File == "" {
			return nil, DiagnosticsOutput{}, fmt.Errorf("file is required")
		}

		absPath, err := filepath.Abs(in.File)
		if err != nil {
			return nil, DiagnosticsOutput{}, fmt.Errorf("resolve file path %q: %w", in.File, err)
		}

		lang := in.Language
		if lang == "" {
			lang = defaultLanguage
		}

		text, err := os.ReadFile(absPath)
		if err != nil {
			return nil, DiagnosticsOutput{}, fmt.Errorf("read file %q: %w", absPath, err)
		}

		diags, err := looker.Lookup(ctx, lang, absPath, string(text))
		if err != nil {
			return nil, DiagnosticsOutput{}, err
		}

		return nil, DiagnosticsOutput{
			File:        absPath,
			Diagnostics: toItems(diags),
		}, nil
	}
}

// toItems converts zero-based domain diagnostics into one-based tool items.
func toItems(diags []lsp.Diagnostic) []DiagnosticItem {
	items := make([]DiagnosticItem, 0, len(diags))
	for _, d := range diags {
		items = append(items, DiagnosticItem{
			Severity:    d.Severity,
			Message:     d.Message,
			Source:      d.Source,
			Code:        d.Code,
			StartLine:   d.StartLine + 1,
			StartColumn: d.StartColumn + 1,
			EndLine:     d.EndLine + 1,
			EndColumn:   d.EndColumn + 1,
		})
	}

	return items
}
