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

package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"go.lsp.dev/protocol"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

// fileContent is the source written to the temporary file used by the handler
// tests; its bytes are forwarded verbatim to the fake looker.
const fileContent = "package main\n"

// writeTempFile writes fileContent to a file in a temp dir and returns its path.
func writeTempFile(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "main.go")
	writeFile(t, path, fileContent)
	return path
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func testResolver(t *testing.T, languages ...string) languageResolver {
	t.Helper()

	cfg := make(map[string]lsp.ServerConfig, len(languages))
	for _, language := range languages {
		switch language {
		case "go":
			cfg[language] = lsp.ServerConfig{Command: "gopls", LanguageID: protocol.LanguageKindGo}
		case "python":
			cfg[language] = lsp.ServerConfig{Command: "pyright-langserver", Args: []string{"--stdio"}, LanguageID: protocol.LanguageKindPython}
		case "rust":
			cfg[language] = lsp.ServerConfig{Command: "rust-analyzer", LanguageID: protocol.LanguageKindRust}
		default:
			cfg[language] = lsp.ServerConfig{Command: language + "-server", LanguageID: protocol.LanguageKind(language)}
		}
	}
	registry, err := lsp.NewRegistry(lsp.DefaultCatalog(), cfg)
	if err != nil {
		t.Fatalf("create test language registry: %v", err)
	}
	return NewLanguageResolver(registry)
}
