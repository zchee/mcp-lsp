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
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.lsp.dev/protocol"
	"golang.org/x/tools/txtar"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

// integrationEnv is the environment variable that opts in to the integration
// tests. It mirrors the gate honored by `make test/integration`.
const integrationEnv = "MCP_LSP_INTEGRATION"

// goplsSettle is how long the harness lets the extracted workspace settle on
// disk before opening documents, so gopls observes a stable module tree.
const goplsSettle = 100 * time.Millisecond

// requireIntegration skips the test unless the integration gate is set and a
// gopls binary is resolvable on PATH.
func requireIntegration(t *testing.T) {
	t.Helper()

	if os.Getenv(integrationEnv) == "" {
		t.Skipf("set %s=1 to run the gopls integration tests", integrationEnv)
	}
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not found on PATH; skipping the gopls integration test")
	}
}

// workspace is an extracted txtar fixture: an absolute temp directory containing
// the archive's files, plus the parsed source of each file keyed by its
// archive-relative name for marker resolution.
type workspace struct {
	dir   string
	files map[string]string
}

// extractFixture parses the named txtar archive under testdata and writes every
// file it contains into a fresh temp directory, creating parent directories as
// needed. It waits a short settle window so gopls observes a stable workspace,
// then returns the extracted workspace.
func extractFixture(t *testing.T, name string) workspace {
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

	dir := t.TempDir()
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

	// Give the filesystem a moment so gopls loads a settled module tree.
	time.Sleep(goplsSettle)
	return workspace{dir: dir, files: files}
}

// path returns the absolute path of the archive file named rel.
func (w workspace) path(rel string) string {
	return filepath.Join(w.dir, filepath.FromSlash(rel))
}

// source returns the original archive contents of the file named rel.
func (w workspace) source(t *testing.T, rel string) string {
	t.Helper()

	src, ok := w.files[rel]
	if !ok {
		t.Fatalf("fixture has no file %q", rel)
	}
	return src
}

// markerPosition resolves a `marker=ident` annotation in the file named rel to
// the zero-based LSP position of the first occurrence of ident on the annotated
// line. It models clicking on the symbol the marker points at.
//
// A line carries a marker when it contains the literal substring
// "<marker>=<ident>" (for example "query=answer"); the queried position is the
// start of the first ident token on that line, which precedes the marker
// comment. Fixtures are ASCII-only, so a rune scan yields LSP UTF-16 columns
// directly.
func (w workspace) markerPosition(t *testing.T, rel, marker, ident string) protocol.Position {
	t.Helper()

	annotation := marker + "=" + ident
	src := w.source(t, rel)
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
			Character: uint32(runeColumn(line, col)),
		}
	}
	t.Fatalf("file %q has no line carrying marker %q", rel, annotation)
	return protocol.Position{}
}

// runeColumn converts a byte offset within line to a rune (LSP UTF-16 for
// ASCII) column.
func runeColumn(line string, byteOffset int) int {
	return len([]rune(line[:byteOffset]))
}

// newManager constructs a real lsp.Manager rooted at the workspace, configured
// for Go via gopls, and registers cleanup that shuts every spawned session down.
func newManager(t *testing.T, w workspace) *lsp.Manager {
	t.Helper()

	mgr := lsp.NewManager(lsp.DefaultConfig(), w.dir, slog.New(slog.DiscardHandler))
	t.Cleanup(func() {
		if err := mgr.Close(t.Context()); err != nil {
			t.Errorf("manager close reported errors: %v", err)
		}
	})
	return mgr
}
