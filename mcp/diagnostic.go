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
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/zchee/mcp-lsp/lsp"
)

// DiagnosticInput is the typed argument shape of the lsp_diagnostics tool. Its
// exported fields and jsonschema tags drive the tool's input schema, which the
// SDK infers and validates before the handler runs.
type DiagnosticInput struct {
	// File is the path to the document to analyze. A relative path is resolved
	// against the process working directory.
	File string `json:"file" jsonschema:"path to the document to analyze; relative paths resolve against the server working directory"`

	// Language is the LSP language identifier for the document (e.g. go, rust,
	// typescript). It defaults to go when empty.
	Language string `json:"language,omitzero" jsonschema:"LSP language identifier such as go, rust, or typescript; defaults to go"`

	// Mode selects the diagnostic model: "push" waits for the server to
	// volunteer diagnostics after the document is opened, "pull" requests them
	// with textDocument/diagnostic. It defaults to push when empty.
	Mode string `json:"mode,omitzero" jsonschema:"diagnostic model to use: push (wait for published diagnostics) or pull (request them); defaults to push"`

	// WaitSeconds bounds the push-model wait. It is ignored in pull mode and
	// defaults to 10 seconds when zero or negative.
	WaitSeconds int `json:"waitSeconds,omitzero" jsonschema:"push-model wait budget in seconds; ignored for pull mode; defaults to 10"`
}

// Diagnostic is the structured form of a single LSP diagnostic, flattened to
// the fields an agent needs. Positions are one-based to match how editors and
// humans refer to source locations.
type Diagnostic struct {
	// Line is the one-based line of the diagnostic's start position.
	Line uint32 `json:"line" jsonschema:"one-based line of the diagnostic start"`

	// Column is the one-based column of the diagnostic's start position.
	Column uint32 `json:"column" jsonschema:"one-based column of the diagnostic start"`

	// EndLine is the one-based line of the diagnostic's end position.
	EndLine uint32 `json:"endLine" jsonschema:"one-based line of the diagnostic end"`

	// EndColumn is the one-based column of the diagnostic's end position.
	EndColumn uint32 `json:"endColumn" jsonschema:"one-based column of the diagnostic end"`

	// Severity is the textual severity: error, warning, info, hint, or unknown.
	Severity string `json:"severity" jsonschema:"diagnostic severity: error, warning, info, hint, or unknown"`

	// Source is the producer of the diagnostic (e.g. the linter or compiler
	// name), when the server provided one.
	Source string `json:"source,omitzero" jsonschema:"the tool that produced the diagnostic, when reported"`

	// Message is the human-readable diagnostic text.
	Message string `json:"message" jsonschema:"the human-readable diagnostic message"`
}

// DiagnosticOutput is the typed result of the lsp_diagnostics tool. Its fields
// drive the tool's output schema and populate the structured result an agent
// receives.
type DiagnosticOutput struct {
	// URI is the file:// URI of the analyzed document.
	URI string `json:"uri" jsonschema:"the file URI the diagnostics apply to"`

	// Mode is the diagnostic model that produced the result: push or pull.
	Mode string `json:"mode" jsonschema:"the diagnostic model used: push or pull"`

	// Unchanged is true only for a pull-model report where the server confirmed
	// the previous result is still accurate; Diagnostics is empty in that case.
	Unchanged bool `json:"unchanged,omitzero" jsonschema:"pull mode only: the server reported the previous result is unchanged"`

	// Diagnostics is the document's current diagnostic set, empty when the
	// document is clean.
	Diagnostics []Diagnostic `json:"diagnostics" jsonschema:"the diagnostics reported for the document"`
}

// handleDiagnostic implements the lsp_diagnostics tool. It resolves the
// requested file, reads its content, delegates collection to the lsp client
// with the requested model, and maps the flattened report into the tool's
// structured output. A returned error is surfaced to the agent as a tool error
// so it can correct the call.
func (s *Server) handleDiagnostic(ctx context.Context, _ *mcp.CallToolRequest, in DiagnosticInput) (*mcp.CallToolResult, DiagnosticOutput, error) {
	if in.File == "" {
		return nil, DiagnosticOutput{}, fmt.Errorf("file is required")
	}

	abs, err := filepath.Abs(in.File)
	if err != nil {
		return nil, DiagnosticOutput{}, fmt.Errorf("resolving %q: %w", in.File, err)
	}
	source, err := os.ReadFile(abs)
	if err != nil {
		return nil, DiagnosticOutput{}, fmt.Errorf("reading %q: %w", abs, err)
	}

	docURI := uri.File(abs)
	languageID := protocol.LanguageKind(in.Language)
	if languageID == "" {
		languageID = protocol.LanguageKindGo
	}

	mode := lsp.Mode(in.Mode)
	if mode != "" && mode != lsp.ModePush && mode != lsp.ModePull {
		return nil, DiagnosticOutput{}, fmt.Errorf("unknown mode %q: want %q or %q", in.Mode, lsp.ModePush, lsp.ModePull)
	}

	report, err := s.client.Diagnostic().Collect(ctx, docURI, languageID, string(source), mode, time.Duration(in.WaitSeconds)*time.Second)
	if err != nil {
		return nil, DiagnosticOutput{}, err
	}

	return nil, toDiagnosticOutput(report), nil
}

// toDiagnosticOutput maps a flattened lsp report into the tool's schema-tagged
// output. The shapes are deliberately distinct: the lsp report is the domain
// model, while DiagnosticOutput carries the jsonschema tags that are an MCP
// concern, so the two are kept apart and bridged here.
func toDiagnosticOutput(report *lsp.Report) DiagnosticOutput {
	out := DiagnosticOutput{
		URI:         string(report.URI),
		Mode:        string(report.Mode),
		Unchanged:   report.Unchanged,
		Diagnostics: make([]Diagnostic, 0, len(report.Diagnostics)),
	}
	for _, d := range report.Diagnostics {
		out.Diagnostics = append(out.Diagnostics, Diagnostic{
			Line:      d.Line,
			Column:    d.Column,
			EndLine:   d.EndLine,
			EndColumn: d.EndColumn,
			Severity:  d.Severity,
			Source:    d.Source,
			Message:   d.Message,
		})
	}
	return out
}
