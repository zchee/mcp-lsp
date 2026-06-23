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
// gopls language server through the shared lsptest harness.
package go_integration_test

import (
	_ "embed"
	"testing"

	"github.com/zchee/mcp-lsp/tests/internal/lsptest"
)

// fixtureArchive is the fixture module the test materializes to disk. Keeping it
// as a txtar archive separates the test data from the test logic and lets the
// fixture diff cleanly in review rather than living as escaped Go strings.
//
//go:embed testdata/fixture.txtar
var fixtureArchive []byte

// TestIntegrationGopls drives the mcp-lsp command against a live gopls. gopls
// answers both diagnostic models, so the broken and clean files are each
// asserted over push and pull. broken.go references an undefined identifier, so
// gopls must report one error-severity diagnostic at its position; clean.go
// compiles, so both models report none.
func TestIntegrationGopls(t *testing.T) {
	wantBroken := []lsptest.Diagnostic{{
		Line:     6,
		Column:   9,
		Severity: "error",
		Message:  "undefined: undefinedSymbol",
	}}

	lsptest.Run(t, lsptest.Config{
		Server:   []string{"gopls", "serve"},
		Language: "go",
		Fixture:  fixtureArchive,
		Cases: map[string]lsptest.Case{
			"error: pull reports the undefined identifier": {
				File: "broken.go",
				Mode: "pull",
				Want: wantBroken,
			},
			"error: push reports the undefined identifier": {
				File: "broken.go",
				Mode: "push",
				Want: wantBroken,
			},
			"success: pull reports a clean file as empty": {
				File: "clean.go",
				Mode: "pull",
			},
			"success: push reports a clean file as empty": {
				File: "clean.go",
				Mode: "push",
			},
		},
	})
}
