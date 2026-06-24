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

package gointegration

import (
	"context"
	"testing"
	"time"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

// definitionAttempts bounds how many times a definition lookup is retried while
// gopls asynchronously loads the package; cross-file resolution in particular
// returns empty until the workspace is analyzed.
const definitionAttempts = 10

// definitionRetryDelay is the pause between definition lookup attempts.
const definitionRetryDelay = 250 * time.Millisecond

func TestIntegrationDiagnosticsReportsCompileError(t *testing.T) {
	requireIntegration(t)

	ws := extractFixture(t, "diagnostics_error.txtar")
	mgr := newManager(t, ws)

	mainFile := ws.path("main.go")
	text := ws.source(t, "main.go")

	diags, err := mgr.Diagnostics().Lookup(t.Context(), "go", mainFile, text)
	if err != nil {
		t.Fatalf("diagnostics lookup: %v", err)
	}
	if len(diags) == 0 {
		t.Fatal("expected at least one diagnostic for a file with a compile error, got none")
	}

	var foundError bool
	for _, d := range diags {
		if d.Severity != "error" {
			continue
		}
		foundError = true
		// Domain positions are zero-based; an error on the call must be past the
		// first line and non-negative.
		if d.StartLine < 0 || d.StartColumn < 0 {
			t.Errorf("diagnostic positions must be non-negative: %+v", d)
		}
	}
	if !foundError {
		t.Errorf("no error-severity diagnostic reported; diagnostics = %+v", diags)
	}
}

func TestIntegrationDefinitionResolvesLocalSymbol(t *testing.T) {
	requireIntegration(t)

	ws := extractFixture(t, "definition_local.txtar")
	mgr := newManager(t, ws)

	mainFile := ws.path("main.go")
	text := ws.source(t, "main.go")
	query := ws.markerPosition(t, "main.go", "query", "answer")
	target := ws.markerPosition(t, "main.go", "target", "answer")
	wantURI := string(uri.File(mainFile))

	defs := lookupDefinition(t, mgr, mainFile, text, query)
	assertResolvesTo(t, defs, wantURI, target)
}

func TestIntegrationDefinitionResolvesAcrossFiles(t *testing.T) {
	requireIntegration(t)

	ws := extractFixture(t, "definition_crossfile.txtar")
	mgr := newManager(t, ws)

	mainFile := ws.path("main.go")
	text := ws.source(t, "main.go")
	query := ws.markerPosition(t, "main.go", "query", "Greeting")
	target := ws.markerPosition(t, "lib.go", "target", "Greeting")
	wantURI := string(uri.File(ws.path("lib.go")))

	defs := lookupDefinition(t, mgr, mainFile, text, query)
	assertResolvesTo(t, defs, wantURI, target)
}

// lookupDefinition drives Definition.Lookup against real gopls, retrying while
// the package is still loading. It fails the test if no definition resolves
// within the attempt budget.
func lookupDefinition(t *testing.T, mgr *lsp.Manager, absPath, text string, pos protocol.Position) []lsp.DefinitionLocation {
	t.Helper()

	var (
		defs    []lsp.DefinitionLocation
		lastErr error
	)
	for range definitionAttempts {
		defs, lastErr = mgr.Definition().Lookup(t.Context(), "go", absPath, text, pos)
		if lastErr == nil && len(defs) > 0 {
			return defs
		}
		if ctxErr := sleepOrCancel(t.Context(), definitionRetryDelay); ctxErr != nil {
			t.Fatalf("context canceled while waiting for gopls: %v", ctxErr)
		}
	}
	t.Fatalf("no definition resolved after %d attempts; last error = %v, defs = %+v", definitionAttempts, lastErr, defs)
	return nil
}

// assertResolvesTo fails unless some definition target points at wantURI with a
// selection range starting at the expected zero-based position.
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

// sleepOrCancel waits for d or returns the context error if ctx is canceled
// first.
func sleepOrCancel(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
