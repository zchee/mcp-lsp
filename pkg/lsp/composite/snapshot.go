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
	"slices"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

// Capability names a language-server capability a composite may depend on.
// The values match the JSON keys a composite reports in its output metadata.
type Capability string

// Capabilities a composite can request.
const (
	CapReferences        Capability = "references"
	CapDefinition        Capability = "definition"
	CapDeclaration       Capability = "declaration"
	CapTypeDefinition    Capability = "typeDefinition"
	CapImplementation    Capability = "implementation"
	CapDocumentSymbol    Capability = "documentSymbol"
	CapCallHierarchy     Capability = "callHierarchy"
	CapTypeHierarchy     Capability = "typeHierarchy"
	CapHover             Capability = "hover"
	CapSignatureHelp     Capability = "signatureHelp"
	CapDocumentHighlight Capability = "documentHighlight"
	CapInlayHint         Capability = "inlayHint"
	CapCodeAction        Capability = "codeAction"
	CapDiagnostics       Capability = "diagnostics"
)

// CapabilityReport partitions the capabilities a composite requested into the
// ones the server advertised and the ones it did not, so a composite can tell
// an agent which legs it could even attempt. Both slices are sorted for
// deterministic output.
type CapabilityReport struct {
	Used    []Capability
	Missing []Capability
}

// Report resolves lang's capabilities and partitions requested against them.
// Diagnostics is always treated as available: every supported server answers
// diagnostics through either the push or the pull path, so it is never
// reported missing.
func Report(ctx context.Context, probe capabilityProbe, lang string, requested []Capability) (CapabilityReport, error) {
	snap, err := probe.CapabilitySnapshot(ctx, lang)
	if err != nil {
		return CapabilityReport{}, err
	}

	report := CapabilityReport{}
	for _, want := range requested {
		if capabilityAdvertised(snap, want) {
			report.Used = append(report.Used, want)
		} else {
			report.Missing = append(report.Missing, want)
		}
	}
	slices.Sort(report.Used)
	slices.Sort(report.Missing)
	return report, nil
}

func capabilityAdvertised(snap lsp.CapabilitySnapshot, want Capability) bool {
	switch want {
	case CapReferences:
		return snap.References
	case CapDefinition:
		// Every supported server answers go-to-definition; it has no dedicated
		// snapshot flag, so treat it as always available.
		return true
	case CapDeclaration:
		return snap.Declaration
	case CapTypeDefinition:
		return snap.TypeDefinition
	case CapImplementation:
		return snap.Implementation
	case CapDocumentSymbol:
		return snap.DocumentSymbol
	case CapCallHierarchy:
		return snap.CallHierarchy
	case CapTypeHierarchy:
		return snap.TypeHierarchy
	case CapHover:
		return snap.Hover
	case CapSignatureHelp:
		return snap.SignatureHelp
	case CapDocumentHighlight:
		return snap.DocumentHighlight
	case CapInlayHint:
		return snap.InlayHint
	case CapCodeAction:
		return snap.CodeAction
	case CapDiagnostics:
		return true
	default:
		return false
	}
}
