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

// Package lsp drives language servers over jsonrpc2 and exposes their
// capabilities as a small, MCP-friendly domain API.
package lsp

import (
	"path/filepath"
	"slices"
	"strings"

	"go.lsp.dev/protocol"
)

const (
	languageGo     = "go"
	languagePython = "python"
	languageRust   = "rust"
)

// ServerConfig describes how to spawn a language server for one language and
// which LSP language identifier its documents carry.
type ServerConfig struct {
	// Command is the language server executable resolved on PATH.
	Command string

	// Args are the arguments passed to [ServerConfig.Command] when spawning the server.
	Args []string

	// LanguageID is the LSP language kind advertised for documents of this
	// language (for example [protocol.LanguageKindGo]).
	LanguageID protocol.LanguageKind
}

// DefaultConfig returns the built-in language registry keyed by the language
// identifier accepted on the MCP tool input (for example "go").
func DefaultConfig() map[string]ServerConfig {
	return map[string]ServerConfig{
		languageGo: {
			Command:    "gopls",
			Args:       nil,
			LanguageID: protocol.LanguageKindGo,
		},
		languagePython: {
			Command:    "pyright-langserver",
			Args:       []string{"--stdio"},
			LanguageID: protocol.LanguageKindPython,
		},
		languageRust: {
			Command:    "rust-analyzer",
			Args:       nil,
			LanguageID: protocol.LanguageKindRust,
		},
	}
}

// CanonicalLanguage returns the canonical registry key for a user-supplied
// language identifier.
func CanonicalLanguage(lang string) string {
	switch normalized := normalizeLanguage(lang); normalized {
	case "py", "pyright", "basedpyright":
		return languagePython
	default:
		return normalized
	}
}

// InferLanguageFromCommand returns the canonical language that is commonly
// served by command.
func InferLanguageFromCommand(command string) (string, bool) {
	name := strings.ToLower(filepath.Base(command))
	switch {
	case strings.Contains(name, "basedpyright"), strings.Contains(name, "pyright"):
		return languagePython, true
	case strings.Contains(name, "rust-analyzer"):
		return languageRust, true
	case strings.Contains(name, "gopls"):
		return languageGo, true
	default:
		return "", false
	}
}

func normalizeLanguage(lang string) string {
	return strings.ToLower(strings.TrimSpace(lang))
}

func cloneConfig(cfg ServerConfig) ServerConfig {
	cfg.Args = slices.Clone(cfg.Args)
	return cfg
}
