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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.lsp.dev/protocol"
	"golang.org/x/tools/txtar"
)

// IntegrationEnv is the environment variable that opts in to integration tests.
const IntegrationEnv = "MCP_LSP_INTEGRATION"

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

	if settle > 0 {
		time.Sleep(settle)
	}
	return Workspace{dir: dir, files: files}
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
			Character: uint32(utf16Column(line, col)),
		}
	}
	t.Fatalf("file %q has no line carrying marker %q", rel, annotation)
	return protocol.Position{}
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

func utf16Column(line string, byteOffset int) int {
	var col int
	for _, r := range line[:byteOffset] {
		if r >= 0x10000 {
			col += 2
			continue
		}
		col++
	}
	return col
}
