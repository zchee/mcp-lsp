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
// rust-analyzer language server through the shared lsptest harness.
package rust_integration_test

import (
	_ "embed"
	"testing"
	"time"

	"github.com/zchee/mcp-lsp/tests/internal/lsptest"
)

// fixtureArchive is the fixture crate the test materializes to disk. Keeping it
// as a txtar archive separates the test data from the test logic and lets the
// fixture diff cleanly in review rather than living as escaped Go strings.
//
//go:embed testdata/fixture.txtar
var fixtureArchive []byte

// TestIntegrationRustAnalyzer drives the mcp-lsp command against a live
// rust-analyzer, which is invoked with no subcommand to speak LSP over stdio.
//
// rust-analyzer differs from gopls and pyright in ways the case encodes. It
// indexes the cargo crate before publishing diagnostics, far slower than the
// others reaching first analysis, so the broken-file case widens the push wait
// past the tool default; the wait is an upper bound, so a fast session still
// returns promptly. Its diagnostic message is two lines, since it joins the
// primary message and the secondary span label with a newline.
//
// Only the push model is exercised: rust-analyzer does not implement the LSP
// pull request and answers textDocument/diagnostic with a cancellation rather
// than a result, so a pull case would assert the server's non-support, not the
// tool. The harness's unknown-mode error subtest covers the error path.
func TestIntegrationRustAnalyzer(t *testing.T) {
	// The push wait must outlast a cold rust-analyzer indexing the crate; the
	// default 10s is not enough. It is a budget, not a fixed delay.
	const pushWaitSeconds = 60

	wantBroken := []lsptest.Diagnostic{{
		Line:     2,
		Column:   13,
		Severity: "error",
		Message:  "cannot find value `undefined_symbol` in this scope\nnot found in this scope",
	}}

	lsptest.Run(t, lsptest.Config{
		Server:   []string{"rust-analyzer"},
		Language: "rust",
		Fixture:  fixtureArchive,
		Timeout:  120 * time.Second,
		Cases: map[string]lsptest.Case{
			"error: push reports the undefined value": {
				File:        "broken.rs",
				Mode:        "push",
				WaitSeconds: pushWaitSeconds,
				Want:        wantBroken,
			},
		},
	})
}
