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
	"fmt"
	"path/filepath"
	"strings"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

type languageResolver interface {
	ResolveFileLanguage(file, text, explicit string) (string, error)
	ResolveToolLanguage(explicit string) (string, error)
}

// LanguageResolver resolves MCP tool language inputs against the active runtime
// LSP registry.
type LanguageResolver struct {
	registry *lsp.Registry
}

// NewLanguageResolver returns a resolver backed by registry.
func NewLanguageResolver(registry *lsp.Registry) *LanguageResolver {
	return &LanguageResolver{registry: registry}
}

// ResolveFileLanguage resolves a file-based tool language. Explicit language
// wins; otherwise the resolver infers from file metadata and requires a
// configured server for the inferred language.
func (r *LanguageResolver) ResolveFileLanguage(file, text, explicit string) (string, error) {
	if r == nil || r.registry == nil {
		return "", fmt.Errorf("no language registry configured")
	}
	if explicit != "" {
		return r.resolveConfigured(explicit)
	}
	if language, ok := r.registry.LanguageForFile(file, text); ok {
		if r.registry.IsConfigured(language) {
			return language, nil
		}
		return "", fmt.Errorf(
			"no language server configured for %s files (%s); configured languages: %s",
			language,
			languageExtensions(r.registry, language),
			configuredLanguages(r.registry),
		)
	}
	ext := filepath.Ext(file)
	if ext == "" {
		ext = "no extension"
	}
	if len(r.registry.ConfiguredLanguages()) == 0 {
		return "", fmt.Errorf("cannot infer language for file %q (%s); no language servers configured", file, ext)
	}
	return "", fmt.Errorf(
		"cannot infer language for file %q (%s); pass language explicitly; configured languages: %s",
		file,
		ext,
		configuredLanguages(r.registry),
	)
}

// ResolveToolLanguage resolves a file-less tool language. Omitted language is
// allowed only when exactly one active server is configured.
func (r *LanguageResolver) ResolveToolLanguage(explicit string) (string, error) {
	if r == nil || r.registry == nil {
		return "", fmt.Errorf("no language registry configured")
	}
	if explicit != "" {
		return r.resolveConfigured(explicit)
	}
	configured := r.registry.ConfiguredLanguages()
	switch len(configured) {
	case 0:
		return "", fmt.Errorf("language is required; no language servers configured")
	case 1:
		return configured[0], nil
	default:
		return "", fmt.Errorf("language is required when multiple language servers are configured: %s", strings.Join(configured, ", "))
	}
}

func (r *LanguageResolver) resolveConfigured(lang string) (string, error) {
	if r == nil || r.registry == nil {
		return "", fmt.Errorf("no language registry configured")
	}
	canonical, ok := r.registry.CanonicalLanguage(lang)
	if !ok {
		return "", fmt.Errorf("unknown language %q", lang)
	}
	if !r.registry.IsConfigured(canonical) {
		return "", fmt.Errorf("no language server configured for language %q; configured languages: %s", canonical, configuredLanguages(r.registry))
	}
	return canonical, nil
}

func configuredLanguages(registry *lsp.Registry) string {
	configured := registry.ConfiguredLanguages()
	if len(configured) == 0 {
		return "(none)"
	}
	return strings.Join(configured, ", ")
}

func languageExtensions(registry *lsp.Registry, language string) string {
	spec, ok := registry.LanguageSpec(language)
	if !ok || len(spec.Extensions) == 0 {
		return "no registered extensions"
	}
	return strings.Join(spec.Extensions, ", ")
}
