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

// Package gointegration contains targeted integration tests that drive the
// pkg/lsp domain API against a real gopls language server. Unlike the in-memory
// fakeServer unit tests in pkg/lsp, these spawn gopls as a subprocess and assert
// on its actual diagnostics and goto-definition behavior.
//
// Fixtures are golang.org/x/tools/txtar archives under testdata: one archive
// bundles a complete Go module (go.mod plus one or more source files) into a
// single, human-readable file. The harness extracts an archive into a temporary
// workspace on disk so gopls can load the package, which is required for
// cross-file resolution that an in-memory overlay alone cannot provide.
//
// Every test is gated twice: it skips unless MCP_LSP_INTEGRATION is set (so the
// default `go test ./...` stays hermetic) and skips when gopls is absent from
// PATH. `make test/integration` sets the gate and runs ./tests/....
package gointegration

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/zchee/mcp-lsp/pkg/lsp"
	"github.com/zchee/mcp-lsp/tests/internal/lsptest"
)

// goplsSettle is how long the harness lets the extracted workspace settle on
// disk before opening documents, so gopls observes a stable module tree.
const goplsSettle = 100 * time.Millisecond

// requireIntegration skips the test unless the integration gate is set and a
// gopls binary is resolvable on PATH.
func requireIntegration(t *testing.T) {
	t.Helper()

	lsptest.RequireIntegration(t, "gopls")
}

// extractFixture parses the named txtar archive under testdata and writes every
// file it contains into a fresh temp directory, creating parent directories as
// needed. It waits a short settle window so gopls observes a stable workspace,
// then returns the extracted workspace.
func extractFixture(t *testing.T, name string) lsptest.Workspace {
	t.Helper()

	return lsptest.ExtractFixture(t, name, goplsSettle)
}

// newManager constructs a real lsp.Manager rooted at the workspace, configured
// for Go via gopls, and registers cleanup that shuts every spawned session down.
func newManager(t *testing.T, w lsptest.Workspace) *lsp.Manager {
	t.Helper()

	mgr := lsp.NewManager(lsp.DefaultConfig(), w.Dir(), slog.New(slog.DiscardHandler))
	t.Cleanup(func() {
		if err := mgr.Close(context.WithoutCancel(t.Context())); err != nil {
			t.Errorf("manager close reported errors: %v", err)
		}
	})
	return mgr
}
