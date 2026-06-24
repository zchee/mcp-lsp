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

// Package rustintegration contains targeted integration tests that drive the
// pkg/lsp domain API against a real rust-analyzer language server.
//
// Fixtures are golang.org/x/tools/txtar archives under testdata. Each archive
// describes a small Cargo workspace that the harness extracts into a temporary
// directory before issuing LSP requests.
//
// Tests are gated twice: they skip unless MCP_LSP_INTEGRATION is set and skip
// when rust-analyzer is absent from PATH.
package rustintegration

import (
	"log/slog"
	"testing"
	"time"

	"go.lsp.dev/protocol"

	"github.com/zchee/mcp-lsp/pkg/lsp"
	"github.com/zchee/mcp-lsp/tests/internal/lsptest"
)

const (
	rustAnalyzerCommand  = "rust-analyzer"
	rustLanguage         = "rust"
	rustSettle           = 250 * time.Millisecond
	definitionAttempts   = 20
	definitionRetryDelay = 250 * time.Millisecond
)

func requireIntegration(t *testing.T) {
	t.Helper()

	lsptest.RequireIntegration(t, rustAnalyzerCommand)
}

func extractFixture(t *testing.T, name string) lsptest.Workspace {
	t.Helper()

	return lsptest.ExtractFixture(t, name, rustSettle)
}

func newManager(t *testing.T, w lsptest.Workspace) *lsp.Manager {
	t.Helper()

	cfg := map[string]lsp.ServerConfig{
		rustLanguage: {
			Command:    rustAnalyzerCommand,
			LanguageID: protocol.LanguageKindRust,
		},
	}
	mgr := lsp.NewManager(cfg, w.Dir(), slog.New(slog.DiscardHandler))
	t.Cleanup(func() {
		if err := mgr.Close(t.Context()); err != nil {
			t.Errorf("manager close reported errors: %v", err)
		}
	})
	return mgr
}

func lookupDefinition(t *testing.T, mgr *lsp.Manager, absPath, text string, pos protocol.Position) []lsp.DefinitionLocation {
	t.Helper()

	var (
		defs    []lsp.DefinitionLocation
		lastErr error
	)
	for range definitionAttempts {
		defs, lastErr = mgr.Definition().Lookup(t.Context(), rustLanguage, absPath, text, pos)
		if lastErr == nil && len(defs) > 0 {
			return defs
		}
		if ctxErr := lsptest.SleepOrCancel(t.Context(), definitionRetryDelay); ctxErr != nil {
			t.Fatalf("context canceled while waiting for rust-analyzer: %v", ctxErr)
		}
	}
	t.Fatalf("no definition resolved after %d attempts; last error = %v, defs = %+v", definitionAttempts, lastErr, defs)
	return nil
}

func assertResolvesTo(t *testing.T, defs []lsp.DefinitionLocation, wantURI string, want protocol.Position) {
	t.Helper()

	for _, def := range defs {
		if def.TargetURI != wantURI {
			continue
		}
		sel := def.TargetSelectionRange
		if uint32(sel.StartLine) == want.Line && uint32(sel.StartColumn) == want.Character {
			return
		}
	}
	t.Fatalf("no definition pointed to %s at %d:%d (zero-based); defs = %+v", wantURI, want.Line, want.Character, defs)
}
