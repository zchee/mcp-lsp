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
	"errors"
	"fmt"
	"log/slog"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func TestErrUnsupportedMatchability(t *testing.T) {
	t.Parallel()

	wrapped := fmt.Errorf("references: %w", ErrUnsupported)
	if !errors.Is(wrapped, ErrUnsupported) {
		t.Errorf("errors.Is(%v, ErrUnsupported) = false, want true", wrapped)
	}
	if errors.Is(errors.New("references: capability not supported by language server"), ErrUnsupported) {
		t.Error("errors.Is matched an unrelated error with identical text; sentinel identity must not be textual")
	}
}

func TestSnapshotCapabilitiesNewFields(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		capabilities *protocol.ServerCapabilities
		want         sessionCapabilities
	}{
		"success: all six advertised as boolean true": {
			capabilities: &protocol.ServerCapabilities{
				ReferencesProvider:     protocol.Boolean(true),
				DeclarationProvider:    protocol.Boolean(true),
				TypeDefinitionProvider: protocol.Boolean(true),
				DocumentSymbolProvider: protocol.Boolean(true),
				CallHierarchyProvider:  protocol.Boolean(true),
				TypeHierarchyProvider:  protocol.Boolean(true),
			},
			want: sessionCapabilities{
				references:     true,
				declaration:    true,
				typeDefinition: true,
				documentSymbol: true,
				callHierarchy:  true,
				typeHierarchy:  true,
			},
		},
		"success: boolean false is not advertised": {
			capabilities: &protocol.ServerCapabilities{
				ReferencesProvider:    protocol.Boolean(false),
				DeclarationProvider:   protocol.Boolean(false),
				CallHierarchyProvider: protocol.Boolean(false),
			},
			want: sessionCapabilities{},
		},
		"success: option structs advertise support": {
			capabilities: &protocol.ServerCapabilities{
				ReferencesProvider:     &protocol.ReferenceOptions{},
				DeclarationProvider:    &protocol.DeclarationOptions{},
				TypeDefinitionProvider: &protocol.TypeDefinitionOptions{},
				DocumentSymbolProvider: &protocol.DocumentSymbolOptions{},
				CallHierarchyProvider:  &protocol.CallHierarchyOptions{},
				TypeHierarchyProvider:  &protocol.TypeHierarchyOptions{},
			},
			want: sessionCapabilities{
				references:     true,
				declaration:    true,
				typeDefinition: true,
				documentSymbol: true,
				callHierarchy:  true,
				typeHierarchy:  true,
			},
		},
		"success: typed nil option pointers are not advertised": {
			capabilities: &protocol.ServerCapabilities{
				ReferencesProvider:     (*protocol.ReferenceOptions)(nil),
				DeclarationProvider:    (*protocol.DeclarationOptions)(nil),
				TypeDefinitionProvider: (*protocol.TypeDefinitionOptions)(nil),
				DocumentSymbolProvider: (*protocol.DocumentSymbolOptions)(nil),
				CallHierarchyProvider:  (*protocol.CallHierarchyOptions)(nil),
				TypeHierarchyProvider:  (*protocol.TypeHierarchyOptions)(nil),
			},
			want: sessionCapabilities{},
		},
		"success: absent providers are not advertised": {
			capabilities: &protocol.ServerCapabilities{},
			want:         sessionCapabilities{},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := snapshotCapabilities(tt.capabilities)
			if got.references != tt.want.references ||
				got.declaration != tt.want.declaration ||
				got.typeDefinition != tt.want.typeDefinition ||
				got.documentSymbol != tt.want.documentSymbol ||
				got.callHierarchy != tt.want.callHierarchy ||
				got.typeHierarchy != tt.want.typeHierarchy {
				t.Errorf("snapshotCapabilities new fields = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestManagerCapabilitySnapshot(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		capabilities sessionCapabilities
		want         CapabilitySnapshot
	}{
		"success: advertised capabilities map onto the exported snapshot": {
			capabilities: sessionCapabilities{
				references:     true,
				typeDefinition: true,
				documentSymbol: true,
				callHierarchy:  true,
				hover:          true,
				implementation: true,
				rename:         true,
				formatting:     true,
			},
			want: CapabilitySnapshot{
				References:     true,
				TypeDefinition: true,
				DocumentSymbol: true,
				CallHierarchy:  true,
				Hover:          true,
				Implementation: true,
				Rename:         true,
				Formatting:     true,
			},
		},
		"success: nothing advertised yields the zero snapshot": {
			capabilities: sessionCapabilities{},
			want:         CapabilitySnapshot{},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			mgr := NewManager(map[string]ServerConfig{
				"go": {Command: "unused", LanguageID: "go"},
			}, t.TempDir(), slog.New(slog.DiscardHandler))
			mgr.newSessionFn = func(store *store, logger *slog.Logger) *serverSession {
				sess := newSession(store, logger)
				sess.startFn = func(context.Context, ServerConfig, uri.URI) {
					sess.capabilities = tt.capabilities
					close(sess.ready)
				}
				return sess
			}

			got, err := mgr.CapabilitySnapshot(t.Context(), "go")
			if err != nil {
				t.Fatalf("CapabilitySnapshot: %v", err)
			}
			if diff := gocmp.Diff(tt.want, got); diff != "" {
				t.Errorf("CapabilitySnapshot mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestManagerCapabilitySnapshotUnknownLanguage(t *testing.T) {
	t.Parallel()

	mgr := NewManager(map[string]ServerConfig{}, t.TempDir(), slog.New(slog.DiscardHandler))
	if _, err := mgr.CapabilitySnapshot(t.Context(), "cobol"); err == nil {
		t.Fatal("CapabilitySnapshot for an unconfigured language succeeded, want error")
	}
}
