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

import (
	"context"
	"fmt"
	"time"

	"go.lsp.dev/protocol"
)

// SignatureHelp issues textDocument/signatureHelp requests through a [Manager].
type SignatureHelp struct {
	mgr     *Manager
	timeout time.Duration
}

// SignatureHelp returns a textDocument/signatureHelp helper for this manager.
func (m *Manager) SignatureHelp() *SignatureHelp {
	return &SignatureHelp{mgr: m, timeout: defaultTimeout}
}

// SignatureInfo is a compact, zero-based signature description.
type SignatureInfo struct {
	Label           string
	Documentation   string
	Parameters      []string
	ActiveParameter int
}

// SignatureHelpResult is a compact signature help response with a zero-based
// active signature index.
type SignatureHelpResult struct {
	Signatures      []SignatureInfo
	ActiveSignature int
}

// Lookup opens absPath, synchronizes its text, and returns signature help at
// pos.
func (s *SignatureHelp) Lookup(ctx context.Context, lang, absPath, text string, pos protocol.Position) (*SignatureHelpResult, error) {
	ctx, cancel := withRequestTimeout(ctx, s.timeout)
	defer cancel()

	sess, languageID, u, err := s.mgr.sessionForFile(ctx, lang, absPath)
	if err != nil {
		return nil, err
	}
	if !sess.capabilities.signatureHelp {
		return nil, fmt.Errorf("signature help request: %w", ErrUnsupported)
	}
	if err := sess.syncTextDocument(ctx, u, languageID, text); err != nil {
		return nil, err
	}

	result, err := sess.server.SignatureHelp(ctx, &protocol.SignatureHelpParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: u},
			Position:     pos,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("signature help request: %w", err)
	}
	return flattenSignatureHelp(result), nil
}

func flattenSignatureHelp(in *protocol.SignatureHelp) *SignatureHelpResult {
	if in == nil {
		return nil
	}
	out := &SignatureHelpResult{Signatures: make([]SignatureInfo, 0, len(in.Signatures))}
	if in.ActiveSignature != nil {
		out.ActiveSignature = int(*in.ActiveSignature)
	}
	for _, sig := range in.Signatures {
		info := SignatureInfo{
			Label:         sig.Label,
			Documentation: signatureDocumentationText(sig.Documentation),
			Parameters:    make([]string, 0, len(sig.Parameters)),
		}
		if v, ok := sig.ActiveParameter.Get(); ok {
			info.ActiveParameter = int(v)
		}
		for _, param := range sig.Parameters {
			info.Parameters = append(info.Parameters, parameterLabelText(param.Label))
		}
		out.Signatures = append(out.Signatures, info)
	}
	return out
}

func signatureDocumentationText(doc protocol.InlayHintTooltip) string {
	switch v := doc.(type) {
	case protocol.String:
		return string(v)
	case *protocol.MarkupContent:
		if v == nil {
			return ""
		}
		return v.Value
	default:
		return ""
	}
}

func parameterLabelText(label protocol.ParameterInformationLabel) string {
	if v, ok := label.(protocol.String); ok {
		return string(v)
	}
	return ""
}
