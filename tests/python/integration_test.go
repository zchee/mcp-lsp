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

// Package integration drives the mcp-lsp command end to end against a real
// pyright language server through the shared lsptest harness.
package python_integration_test

import (
	_ "embed"
	"testing"

	"github.com/zchee/mcp-lsp/tests/internal/lsptest"
)

// fixtureArchive is the fixture project the test materializes to disk. Keeping
// it as a txtar archive separates the test data from the test logic and lets the
// fixture diff cleanly in review rather than living as escaped Go strings.
//
//go:embed testdata/fixture.txtar
var fixtureArchive []byte

// TestIntegrationPyright drives the mcp-lsp command against a live pyright.
// pyright-langserver speaks LSP only with --stdio; without it the process exits
// before any handshake. It answers both diagnostic models, so the broken and
// clean files are each asserted over push and pull. broken.py references an
// undefined name, so pyright must report one error-severity diagnostic at its
// position; clean.py type-checks, so both models report none.
func TestIntegrationPyright(t *testing.T) {
	wantBroken := []lsptest.Diagnostic{{
		Line:     2,
		Column:   12,
		Severity: "error",
		Message:  `"undefined_symbol" is not defined`,
	}}

	lsptest.Run(t, &lsptest.Config{
		Server:   []string{"pyright-langserver", "--stdio"},
		Language: "python",
		Fixture:  fixtureArchive,
		Cases: map[string]lsptest.Case{
			"error: pull reports the undefined name": {
				File: "broken.py",
				Mode: "pull",
				Want: wantBroken,
			},
			"error: push reports the undefined name": {
				File: "broken.py",
				Mode: "push",
				Want: wantBroken,
			},
			"success: pull reports a clean file as empty": {
				File: "clean.py",
				Mode: "pull",
			},
			"success: push reports a clean file as empty": {
				File: "clean.py",
				Mode: "push",
			},
		},
	})
}
