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

// Package pythonintegration contains targeted integration tests that drive the
// pkg/lsp domain API against a real pyright language server.
//
// Fixtures are golang.org/x/tools/txtar archives under testdata. Each archive
// describes a small Python workspace with a deterministic pyrightconfig.json
// that the harness extracts into a temporary directory before issuing LSP
// requests.
//
// Tests are gated twice: they skip unless MCP_LSP_INTEGRATION is set and skip
// when pyright-langserver is absent from PATH.
package pythonintegration

import (
	"testing"
	"time"

	"go.lsp.dev/protocol"

	"github.com/zchee/mcp-lsp/pkg/lsp"
	"github.com/zchee/mcp-lsp/tests/internal/lsptest"
)

const (
	pyrightCommand = "pyright-langserver"
	pythonLanguage = "python"
	pyrightSettle  = 250 * time.Millisecond
)

var pyrightFeatureLookup = lsptest.LookupConfig{
	Language:   pythonLanguage,
	ServerName: pyrightCommand,
	Attempts:   20,
	RetryDelay: 250 * time.Millisecond,
}

func requireIntegration(t *testing.T) {
	t.Helper()

	lsptest.RequireIntegration(t, pyrightCommand)
}

func extractFixture(t *testing.T, name string) lsptest.Workspace {
	t.Helper()

	return lsptest.ExtractFixture(t, name, pyrightSettle)
}

func newManager(t *testing.T, w lsptest.Workspace) *lsp.Manager {
	t.Helper()

	cfg := map[string]lsp.ServerConfig{
		pythonLanguage: {
			Command:    pyrightCommand,
			Args:       []string{"--stdio"},
			LanguageID: protocol.LanguageKindPython,
		},
	}
	return lsptest.NewManager(t, cfg, w)
}
