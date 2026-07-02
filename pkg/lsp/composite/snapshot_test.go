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
	"errors"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

// fakeProbe is a capabilityProbe test double returning a canned snapshot or
// error.
type fakeProbe struct {
	snap lsp.CapabilitySnapshot
	err  error
}

func (f fakeProbe) CapabilitySnapshot(context.Context, string) (lsp.CapabilitySnapshot, error) {
	return f.snap, f.err
}

func TestReportPartitionsRequested(t *testing.T) {
	t.Parallel()

	// A gopls-like server: type hierarchy yes, declaration no.
	gopls := lsp.CapabilitySnapshot{
		References:     true,
		TypeDefinition: true,
		DocumentSymbol: true,
		CallHierarchy:  true,
		TypeHierarchy:  true,
		Hover:          true,
	}
	// A rust-analyzer-like server: declaration yes, type hierarchy no.
	rustAnalyzer := lsp.CapabilitySnapshot{
		References:     true,
		Declaration:    true,
		TypeDefinition: true,
		DocumentSymbol: true,
		CallHierarchy:  true,
		Hover:          true,
	}

	requested := []Capability{
		CapReferences, CapDefinition, CapDeclaration, CapTypeDefinition,
		CapDocumentSymbol, CapCallHierarchy, CapTypeHierarchy, CapHover, CapDiagnostics,
	}

	tests := map[string]struct {
		snap        lsp.CapabilitySnapshot
		wantUsed    []Capability
		wantMissing []Capability
	}{
		"success: gopls advertises type hierarchy but not declaration": {
			snap:        gopls,
			wantUsed:    []Capability{CapCallHierarchy, CapDefinition, CapDiagnostics, CapDocumentSymbol, CapHover, CapReferences, CapTypeDefinition, CapTypeHierarchy},
			wantMissing: []Capability{CapDeclaration},
		},
		"success: rust-analyzer advertises declaration but not type hierarchy": {
			snap:        rustAnalyzer,
			wantUsed:    []Capability{CapCallHierarchy, CapDeclaration, CapDefinition, CapDiagnostics, CapDocumentSymbol, CapHover, CapReferences, CapTypeDefinition},
			wantMissing: []Capability{CapTypeHierarchy},
		},
		"success: bare server keeps only the always-available capabilities": {
			snap:        lsp.CapabilitySnapshot{},
			wantUsed:    []Capability{CapDefinition, CapDiagnostics},
			wantMissing: []Capability{CapCallHierarchy, CapDeclaration, CapDocumentSymbol, CapHover, CapReferences, CapTypeDefinition, CapTypeHierarchy},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := Report(t.Context(), fakeProbe{snap: tt.snap}, "go", requested)
			if err != nil {
				t.Fatalf("Report: %v", err)
			}
			if diff := gocmp.Diff(tt.wantUsed, got.Used); diff != "" {
				t.Errorf("Used mismatch (-want +got):\n%s", diff)
			}
			if diff := gocmp.Diff(tt.wantMissing, got.Missing); diff != "" {
				t.Errorf("Missing mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestReportPropagatesProbeError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("session spawn failed")
	_, err := Report(t.Context(), fakeProbe{err: sentinel}, "go", []Capability{CapReferences})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Report error = %v, want %v", err, sentinel)
	}
}

func TestReportDeterministicOrder(t *testing.T) {
	t.Parallel()

	snap := lsp.CapabilitySnapshot{References: true, Hover: true}
	requested := []Capability{CapHover, CapReferences, CapTypeHierarchy, CapCallHierarchy}

	first, err := Report(t.Context(), fakeProbe{snap: snap}, "go", requested)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	second, err := Report(t.Context(), fakeProbe{snap: snap}, "go", requested)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if diff := gocmp.Diff(first, second); diff != "" {
		t.Errorf("Report is not deterministic (-first +second):\n%s", diff)
	}
}
