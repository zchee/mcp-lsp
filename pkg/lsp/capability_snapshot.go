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

import "context"

// CapabilitySnapshot is a read-only view of the capabilities the language
// server for one language advertised during the initialize handshake. Reading
// it is race-free: session capabilities are written once before the session
// signals ready and never mutated afterwards.
type CapabilitySnapshot struct {
	References        bool
	Declaration       bool
	TypeDefinition    bool
	DocumentSymbol    bool
	CallHierarchy     bool
	TypeHierarchy     bool
	SignatureHelp     bool
	DocumentHighlight bool
	InlayHint         bool
	FoldingRange      bool
	Hover             bool
	Implementation    bool
	Rename            bool
	CodeAction        bool
	CodeLens          bool
	Formatting        bool
	RangeFormatting   bool
	WorkspaceSymbol   bool
	PullDiagnostics   bool
}

// CapabilitySnapshot returns the advertised capabilities for lang, spawning
// its language server on first use.
func (m *Manager) CapabilitySnapshot(ctx context.Context, lang string) (CapabilitySnapshot, error) {
	sess, err := m.session(ctx, lang)
	if err != nil {
		return CapabilitySnapshot{}, err
	}
	c := sess.capabilities
	return CapabilitySnapshot{
		References:        c.references,
		Declaration:       c.declaration,
		TypeDefinition:    c.typeDefinition,
		DocumentSymbol:    c.documentSymbol,
		CallHierarchy:     c.callHierarchy,
		TypeHierarchy:     c.typeHierarchy,
		SignatureHelp:     c.signatureHelp,
		DocumentHighlight: c.documentHighlight,
		InlayHint:         c.inlayHint,
		FoldingRange:      c.foldingRange,
		Hover:             c.hover,
		Implementation:    c.implementation,
		Rename:            c.rename,
		CodeAction:        c.codeAction,
		CodeLens:          c.codeLens,
		Formatting:        c.formatting,
		RangeFormatting:   c.rangeFormatting,
		WorkspaceSymbol:   c.workspaceSymbol,
		PullDiagnostics:   c.pullDiagnostics,
	}, nil
}
