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

// Package lsptest provides small shared helpers for opt-in integration tests
// that extract txtar workspaces and drive real language servers.
package lsptest

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"go.lsp.dev/protocol"
	"golang.org/x/tools/txtar"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

// IntegrationEnv is the environment variable that opts in to integration tests.
const IntegrationEnv = "MCP_LSP_INTEGRATION"

const (
	workspaceCleanupAttempts = 50
	workspaceCleanupDelay    = 100 * time.Millisecond
)

// LookupConfig controls retry behavior for real language-server navigation
// lookups.
type LookupConfig struct {
	Language   string
	ServerName string
	Attempts   int
	RetryDelay time.Duration
}

// DefinitionLookupConfig controls retry behavior for real language-server
// definition lookups.
type DefinitionLookupConfig = LookupConfig

// ImplementationLookupConfig controls retry behavior for real language-server
// implementation lookups.
type ImplementationLookupConfig = LookupConfig

// RequireIntegration skips t unless the integration gate is set and serverName
// is resolvable on PATH.
func RequireIntegration(t *testing.T, serverName string) {
	t.Helper()

	if os.Getenv(IntegrationEnv) == "" {
		t.Skipf("set %s=1 to run the %s integration tests", IntegrationEnv, serverName)
	}
	if _, err := exec.LookPath(serverName); err != nil {
		t.Skipf("%s not found on PATH; skipping the %s integration test", serverName, serverName)
	}
}

// Workspace is an extracted txtar fixture: an absolute temp directory
// containing the archive files plus source text keyed by archive-relative name.
type Workspace struct {
	dir   string
	files map[string]string
}

// ExtractFixture parses the named txtar archive under testdata, writes its
// files into a fresh temp directory, waits for settle, and returns the workspace.
func ExtractFixture(t *testing.T, name string, settle time.Duration) Workspace {
	t.Helper()

	path := filepath.Join("testdata", name)
	archiveBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %q: %v", path, err)
	}

	archive := txtar.Parse(archiveBytes)
	if len(archive.Files) == 0 {
		t.Fatalf("fixture %q contains no files", path)
	}

	dir, err := os.MkdirTemp("", "mcp-lsp-fixture-*")
	if err != nil {
		t.Fatalf("create fixture workspace: %v", err)
	}
	t.Cleanup(func() {
		cleanupWorkspace(t, dir)
	})

	files := make(map[string]string, len(archive.Files))
	for _, f := range archive.Files {
		dest := filepath.Join(dir, filepath.FromSlash(f.Name))
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			t.Fatalf("create dir for %q: %v", f.Name, err)
		}
		if err := os.WriteFile(dest, f.Data, 0o600); err != nil {
			t.Fatalf("write fixture file %q: %v", f.Name, err)
		}
		files[f.Name] = string(f.Data)
	}

	if settle > 0 {
		time.Sleep(settle)
	}
	return Workspace{dir: dir, files: files}
}

func cleanupWorkspace(t *testing.T, dir string) {
	t.Helper()

	var firstErr error
	for attempt := range workspaceCleanupAttempts {
		err := os.RemoveAll(dir)
		if err == nil || os.IsNotExist(err) {
			if attempt > 0 {
				t.Logf("removed fixture workspace %s after %d retries; first error: %v", dir, attempt, firstErr)
			}
			return
		}
		if firstErr == nil {
			firstErr = err
		}
		time.Sleep(workspaceCleanupDelay)
	}
	t.Fatalf("remove fixture workspace %s after %d attempts: first error: %v", dir, workspaceCleanupAttempts, firstErr)
}

// Dir returns the absolute extracted workspace root directory.
func (w Workspace) Dir() string {
	return w.dir
}

// Path returns the absolute path of the archive file named rel.
func (w Workspace) Path(rel string) string {
	return filepath.Join(w.dir, filepath.FromSlash(rel))
}

// Source returns the original archive contents of the file named rel.
func (w Workspace) Source(t *testing.T, rel string) string {
	t.Helper()

	src, ok := w.files[rel]
	if !ok {
		t.Fatalf("fixture has no file %q", rel)
	}
	return src
}

// MarkerPosition resolves a `marker=ident` annotation in rel to the zero-based
// LSP position of the first occurrence of ident on the annotated line. The
// marker convention is intentionally fixture-oriented: the annotated line must
// contain both the queried token text and a trailing marker comment such as
// `query=Greeting`, and the returned character offset uses LSP's default UTF-16
// code-unit columns.
func (w Workspace) MarkerPosition(t *testing.T, rel, marker, ident string) protocol.Position {
	t.Helper()

	annotation := marker + "=" + ident
	src := w.Source(t, rel)
	for lineIndex, line := range strings.Split(src, "\n") {
		if !strings.Contains(line, annotation) {
			continue
		}
		col := strings.Index(line, ident)
		if col < 0 {
			t.Fatalf("file %q line %d carries marker %q but no %q token", rel, lineIndex+1, annotation, ident)
		}
		return protocol.Position{
			Line:      uint32(lineIndex),
			Character: utf16Column(line, col),
		}
	}
	t.Fatalf("file %q has no line carrying marker %q", rel, annotation)
	return protocol.Position{}
}

// NewManager constructs an [lsp.Manager] rooted at w and registers cleanup that
// can outlive test-context cancellation.
func NewManager(t *testing.T, cfg map[string]lsp.ServerConfig, w Workspace) *lsp.Manager {
	t.Helper()

	mgr := lsp.NewManager(cfg, w.Dir(), slog.New(slog.DiscardHandler))
	t.Cleanup(func() {
		if err := mgr.Close(context.WithoutCancel(t.Context())); err != nil {
			t.Errorf("manager close reported errors: %v", err)
		}
	})
	return mgr
}

// LookupDefinition drives [lsp.Definition.Lookup] against a real language
// server, retrying while the server is still loading the workspace. It fails
// the test if no definition resolves within cfg's attempt budget.
func LookupDefinition(t *testing.T, mgr *lsp.Manager, cfg DefinitionLookupConfig, absPath, text string, pos protocol.Position) []lsp.NavigationLocation {
	t.Helper()
	validateLookupConfig(t, "definition", cfg)

	var (
		defs    []lsp.NavigationLocation
		lastErr error
	)
	for range cfg.Attempts {
		defs, lastErr = mgr.Definition().Lookup(t.Context(), cfg.Language, absPath, text, pos)
		if lastErr == nil && len(defs) > 0 {
			return defs
		}
		if ctxErr := SleepOrCancel(t.Context(), cfg.RetryDelay); ctxErr != nil {
			t.Fatalf("context canceled while waiting for %s: %v", cfg.ServerName, ctxErr)
		}
	}
	t.Fatalf("no definition resolved after %d attempts; last error = %v, defs = %+v", cfg.Attempts, lastErr, defs)
	return nil
}

// LookupImplementation drives [lsp.Implementation.Lookup] against a real
// language server, retrying while the server is still loading the workspace. It
// fails the test if no implementation resolves within cfg's attempt budget.
func LookupImplementation(t *testing.T, mgr *lsp.Manager, cfg ImplementationLookupConfig, absPath, text string, pos protocol.Position) []lsp.NavigationLocation {
	t.Helper()
	validateLookupConfig(t, "implementation", cfg)

	var (
		implementations []lsp.NavigationLocation
		lastErr         error
	)
	for range cfg.Attempts {
		implementations, lastErr = mgr.Implementation().Lookup(t.Context(), cfg.Language, absPath, text, pos)
		if lastErr == nil && len(implementations) > 0 {
			return implementations
		}
		if ctxErr := SleepOrCancel(t.Context(), cfg.RetryDelay); ctxErr != nil {
			t.Fatalf("context canceled while waiting for %s: %v", cfg.ServerName, ctxErr)
		}
	}
	t.Fatalf("no implementation resolved after %d attempts; last error = %v, implementations = %+v", cfg.Attempts, lastErr, implementations)
	return nil
}

// AssertDefinitionResolvesTo fails unless some definition target points at
// wantURI with a selection range starting at the expected zero-based position.
func AssertDefinitionResolvesTo(t *testing.T, defs []lsp.NavigationLocation, wantURI string, want protocol.Position) {
	t.Helper()

	assertNavigationResolvesTo(t, "definition", defs, wantURI, want)
}

// AssertImplementationResolvesTo fails unless some implementation target points
// at wantURI with a selection range starting at the expected zero-based
// position.
func AssertImplementationResolvesTo(t *testing.T, implementations []lsp.NavigationLocation, wantURI string, want protocol.Position) {
	t.Helper()

	assertNavigationResolvesTo(t, "implementation", implementations, wantURI, want)
}

// AssertTextEditForURI reports a test failure unless edit contains a text edit
// for wantURI and all returned edit ranges are non-negative.
func AssertTextEditForURI(tb testing.TB, label string, edit lsp.WorkspaceEdit, wantURI string) []lsp.WorkspaceTextEdit {
	tb.Helper()

	edits := textEditsForURI(edit, wantURI)
	if len(edits) == 0 {
		tb.Fatalf("%s returned no text edits for %s; edit = %+v", label, wantURI, edit)
	}
	for _, te := range edits {
		if te.Range.StartLine < 0 || te.Range.StartColumn < 0 || te.Range.EndLine < 0 || te.Range.EndColumn < 0 {
			tb.Fatalf("%s edit has negative zero-based range: %+v", label, te)
		}
	}
	return edits
}

func textEditsForURI(edit lsp.WorkspaceEdit, wantURI string) []lsp.WorkspaceTextEdit {
	out := slices.Clone(edit.Changes[wantURI])
	for _, change := range edit.DocumentChanges {
		if change.TextDocumentEdit == nil || change.TextDocumentEdit.TextDocument.URI != wantURI {
			continue
		}
		out = append(out, change.TextDocumentEdit.Edits...)
	}
	return out
}

// WorkspaceEditHasTextEdits reports whether edit contains at least one text
// edit in either changes representation.
func WorkspaceEditHasTextEdits(edit lsp.WorkspaceEdit) bool {
	for _, edits := range edit.Changes {
		if len(edits) > 0 {
			return true
		}
	}
	for _, change := range edit.DocumentChanges {
		if change.TextDocumentEdit != nil && len(change.TextDocumentEdit.Edits) > 0 {
			return true
		}
	}
	return false
}

// AssertWorkspaceSymbol reports a test failure unless symbols contain wantName
// at wantURI with a non-negative zero-based range.
func AssertWorkspaceSymbol(tb testing.TB, symbols []lsp.WorkspaceSymbol, wantName, wantURI string) {
	tb.Helper()

	for _, symbol := range symbols {
		if symbol.Name != wantName || symbol.URI != wantURI {
			continue
		}
		if symbol.Range == nil {
			tb.Fatalf("workspace symbol %q has nil range: %+v", wantName, symbol)
		}
		if symbol.Range.StartLine < 0 || symbol.Range.StartColumn < 0 {
			tb.Fatalf("workspace symbol range must be zero-based and non-negative: %+v", symbol.Range)
		}
		return
	}
	tb.Fatalf("no workspace symbol %q at %s; symbols = %+v", wantName, wantURI, symbols)
}

// SleepOrCancel waits for d or returns the context error if ctx is canceled
// first.
func SleepOrCancel(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func validateLookupConfig(t *testing.T, kind string, cfg LookupConfig) {
	t.Helper()

	if cfg.Language == "" {
		t.Fatalf("%s lookup language is empty", kind)
	}
	if cfg.ServerName == "" {
		t.Fatalf("%s lookup server name is empty", kind)
	}
	if cfg.Attempts <= 0 {
		t.Fatalf("%s lookup attempts must be positive: %d", kind, cfg.Attempts)
	}
	if cfg.RetryDelay <= 0 {
		t.Fatalf("%s lookup retry delay must be positive: %v", kind, cfg.RetryDelay)
	}
}

func assertNavigationResolvesTo(t *testing.T, kind string, locations []lsp.NavigationLocation, wantURI string, want protocol.Position) {
	t.Helper()

	for _, loc := range locations {
		if loc.TargetURI != wantURI {
			continue
		}
		sel := loc.TargetSelectionRange
		if int64(sel.StartLine) == int64(want.Line) && int64(sel.StartColumn) == int64(want.Character) {
			return
		}
	}
	t.Fatalf("no %s pointed to %s at %d:%d (zero-based); locations = %+v", kind, wantURI, want.Line, want.Character, locations)
}

func utf16Column(line string, byteOffset int) uint32 {
	var col uint32
	for _, r := range line[:byteOffset] {
		if r >= 0x10000 {
			col += 2
			continue
		}
		col++
	}
	return col
}
