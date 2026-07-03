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
	CapFoldingRange      Capability = "foldingRange"
	CapCodeAction        Capability = "codeAction"
	CapCodeLens          Capability = "codeLens"
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

// alwaysAvailable names capabilities every supported server answers even
// though the snapshot carries no dedicated flag for them: go-to-definition has
// no distinct provider bit, and diagnostics are always reachable through either
// the push or the pull path.
var alwaysAvailable = map[Capability]struct{}{
	CapDefinition:  {},
	CapDiagnostics: {},
}

// snapshotFlag maps each snapshot-backed capability to its flag in a
// [lsp.CapabilitySnapshot]. Capabilities in alwaysAvailable are intentionally
// absent here.
var snapshotFlag = map[Capability]func(lsp.CapabilitySnapshot) bool{
	CapReferences:        func(s lsp.CapabilitySnapshot) bool { return s.References },
	CapDeclaration:       func(s lsp.CapabilitySnapshot) bool { return s.Declaration },
	CapTypeDefinition:    func(s lsp.CapabilitySnapshot) bool { return s.TypeDefinition },
	CapImplementation:    func(s lsp.CapabilitySnapshot) bool { return s.Implementation },
	CapDocumentSymbol:    func(s lsp.CapabilitySnapshot) bool { return s.DocumentSymbol },
	CapCallHierarchy:     func(s lsp.CapabilitySnapshot) bool { return s.CallHierarchy },
	CapTypeHierarchy:     func(s lsp.CapabilitySnapshot) bool { return s.TypeHierarchy },
	CapHover:             func(s lsp.CapabilitySnapshot) bool { return s.Hover },
	CapSignatureHelp:     func(s lsp.CapabilitySnapshot) bool { return s.SignatureHelp },
	CapDocumentHighlight: func(s lsp.CapabilitySnapshot) bool { return s.DocumentHighlight },
	CapInlayHint:         func(s lsp.CapabilitySnapshot) bool { return s.InlayHint },
	CapFoldingRange:      func(s lsp.CapabilitySnapshot) bool { return s.FoldingRange },
	CapCodeAction:        func(s lsp.CapabilitySnapshot) bool { return s.CodeAction },
	CapCodeLens:          func(s lsp.CapabilitySnapshot) bool { return s.CodeLens },
}

func capabilityAdvertised(snap lsp.CapabilitySnapshot, want Capability) bool {
	if _, ok := alwaysAvailable[want]; ok {
		return true
	}
	if flag, ok := snapshotFlag[want]; ok {
		return flag(snap)
	}
	return false
}
