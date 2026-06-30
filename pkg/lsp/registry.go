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

package lsp

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"go.lsp.dev/protocol"
)

// Registry is the runtime language registry. It combines catalog metadata with
// active server configs from configuration, discovery, or CLI overrides.
type Registry struct {
	specs       map[string]LanguageSpec
	servers     map[string]ServerConfig
	aliases     map[string]string
	extensions  map[string]string
	shebangs    map[string]string
	shebangKeys []string
}

// NewRegistry returns a runtime registry built from language specs and active
// server configs.
func NewRegistry(specs []LanguageSpec, servers map[string]ServerConfig) (*Registry, error) {
	r := &Registry{
		specs:      make(map[string]LanguageSpec),
		servers:    make(map[string]ServerConfig),
		aliases:    make(map[string]string),
		extensions: make(map[string]string),
		shebangs:   make(map[string]string),
	}
	for i := range specs {
		if err := r.addSpec(&specs[i]); err != nil {
			return nil, err
		}
	}
	if err := r.rebuildIndexes(); err != nil {
		return nil, err
	}
	var addedNewSpec bool
	for lang, cfg := range servers {
		canonical, ok := r.CanonicalLanguage(lang)
		if !ok {
			canonical = normalizeLanguage(lang)
			if canonical == "" {
				return nil, fmt.Errorf("server language is required")
			}
			if err := r.addSpec(&LanguageSpec{Language: canonical, LanguageID: protocol.LanguageKind(canonical)}); err != nil {
				return nil, err
			}
			addedNewSpec = true
		}
		if cfg.Command == "" {
			return nil, fmt.Errorf("server command is required for language %q", canonical)
		}
		cfg = cloneConfig(cfg)
		if cfg.LanguageID == "" {
			spec := r.specs[canonical]
			cfg.LanguageID = spec.LanguageID
			if cfg.LanguageID == "" {
				cfg.LanguageID = protocol.LanguageKind(canonical)
			}
		}
		r.servers[canonical] = cfg
	}
	if addedNewSpec {
		if err := r.rebuildIndexes(); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// CanonicalLanguage returns the registry canonical language for lang. Unknown
// or empty identifiers return ok=false.
func (r *Registry) CanonicalLanguage(lang string) (canonical string, ok bool) {
	if r == nil {
		return "", false
	}
	normalized := normalizeLanguage(lang)
	if normalized == "" {
		return "", false
	}
	canonical, ok = r.aliases[normalized]
	if ok {
		return canonical, true
	}
	if _, ok := r.specs[normalized]; ok {
		return normalized, true
	}
	return "", false
}

// ServerConfig returns the active server config for lang.
func (r *Registry) ServerConfig(lang string) (ServerConfig, bool) {
	canonical, ok := r.CanonicalLanguage(lang)
	if !ok {
		return ServerConfig{}, false
	}
	cfg, ok := r.servers[canonical]
	if !ok {
		return ServerConfig{}, false
	}
	return cloneConfig(cfg), true
}

// ServerConfigs returns all active server configs keyed by canonical language.
func (r *Registry) ServerConfigs() map[string]ServerConfig {
	if r == nil {
		return nil
	}
	cfg := make(map[string]ServerConfig, len(r.servers))
	for lang, serverCfg := range r.servers {
		cfg[lang] = cloneConfig(serverCfg)
	}
	return cfg
}

// ConfiguredLanguages returns canonical languages that have an active server.
func (r *Registry) ConfiguredLanguages() []string {
	if r == nil {
		return nil
	}
	languages := make([]string, 0, len(r.servers))
	for lang := range r.servers {
		languages = append(languages, lang)
	}
	slices.Sort(languages)
	return languages
}

// KnownLanguages returns canonical languages known to the registry catalog.
func (r *Registry) KnownLanguages() []string {
	if r == nil {
		return nil
	}
	languages := make([]string, 0, len(r.specs))
	for lang := range r.specs {
		languages = append(languages, lang)
	}
	slices.Sort(languages)
	return languages
}

// IsConfigured reports whether lang has an active server config.
func (r *Registry) IsConfigured(lang string) bool {
	_, ok := r.ServerConfig(lang)
	return ok
}

// LanguageForFile returns the catalog language implied by file extension or a
// simple shebang match. The returned language may not have an active server.
func (r *Registry) LanguageForFile(file, text string) (language string, ok bool) {
	if r == nil {
		return "", false
	}
	if ext := normalizeExtension(filepath.Ext(file)); ext != "" {
		if lang, ok := r.extensions[ext]; ok {
			return lang, true
		}
	}
	if strings.HasPrefix(text, "#!") {
		firstLine, _, _ := strings.Cut(text, "\n")
		lower := strings.ToLower(firstLine)
		for _, shebang := range r.shebangKeys {
			if strings.Contains(lower, shebang) {
				return r.shebangs[shebang], true
			}
		}
	}
	return "", false
}

// LanguageSpec returns the catalog spec for lang.
func (r *Registry) LanguageSpec(lang string) (LanguageSpec, bool) {
	canonical, ok := r.CanonicalLanguage(lang)
	if !ok {
		return LanguageSpec{}, false
	}
	spec, ok := r.specs[canonical]
	if !ok {
		return LanguageSpec{}, false
	}
	return cloneLanguageSpec(&spec), true
}

func (r *Registry) addSpec(spec *LanguageSpec) error {
	canonical := normalizeLanguage(spec.Language)
	if canonical == "" {
		return fmt.Errorf("language spec language is required")
	}
	cloned := cloneLanguageSpec(spec)
	cloned.Language = canonical
	if cloned.LanguageID == "" {
		cloned.LanguageID = protocol.LanguageKind(canonical)
	}
	for i, alias := range cloned.Aliases {
		alias = normalizeLanguage(alias)
		if alias == "" {
			return fmt.Errorf("empty alias for language %q", canonical)
		}
		cloned.Aliases[i] = alias
	}
	for i, ext := range cloned.Extensions {
		ext = normalizeExtension(ext)
		if ext == "" {
			return fmt.Errorf("empty extension for language %q", canonical)
		}
		cloned.Extensions[i] = ext
	}
	for i, shebang := range cloned.Shebangs {
		shebang = strings.ToLower(strings.TrimSpace(shebang))
		if shebang == "" {
			return fmt.Errorf("empty shebang for language %q", canonical)
		}
		cloned.Shebangs[i] = shebang
	}

	if existing, ok := r.specs[canonical]; ok {
		cloned = mergeLanguageSpec(&existing, &cloned)
	}
	r.specs[canonical] = cloned
	return nil
}

func (r *Registry) rebuildIndexes() error {
	aliases := make(map[string]string, len(r.specs))
	extensions := make(map[string]string)
	shebangs := make(map[string]string)
	for canonical := range r.specs {
		spec := r.specs[canonical]
		if previous, exists := aliases[canonical]; exists && previous != canonical {
			return fmt.Errorf("language %q conflicts with alias for %q", canonical, previous)
		}
		aliases[canonical] = canonical
		for _, alias := range spec.Aliases {
			if previous, exists := aliases[alias]; exists && previous != canonical {
				return fmt.Errorf("alias %q maps to both %q and %q", alias, previous, canonical)
			}
			aliases[alias] = canonical
		}
		for _, ext := range spec.Extensions {
			if previous, exists := extensions[ext]; exists && previous != canonical {
				return fmt.Errorf("extension %q maps to both %q and %q", ext, previous, canonical)
			}
			extensions[ext] = canonical
		}
		for _, shebang := range spec.Shebangs {
			if previous, exists := shebangs[shebang]; exists && previous != canonical {
				return fmt.Errorf("shebang %q maps to both %q and %q", shebang, previous, canonical)
			}
			shebangs[shebang] = canonical
		}
	}
	shebangKeys := make([]string, 0, len(shebangs))
	for shebang := range shebangs {
		shebangKeys = append(shebangKeys, shebang)
	}
	// Match longer shebangs first so a more specific key (e.g. "python3")
	// wins over a prefix key (e.g. "python") and matching stays deterministic
	// regardless of map iteration order.
	slices.SortFunc(shebangKeys, func(a, b string) int {
		if d := len(b) - len(a); d != 0 {
			return d
		}
		return strings.Compare(a, b)
	})
	r.aliases = aliases
	r.extensions = extensions
	r.shebangs = shebangs
	r.shebangKeys = shebangKeys
	return nil
}

func mergeLanguageSpec(base, overlay *LanguageSpec) LanguageSpec {
	merged := cloneLanguageSpec(base)
	if overlay.LanguageID != "" {
		merged.LanguageID = overlay.LanguageID
	}
	merged.Aliases = appendUnique(merged.Aliases, overlay.Aliases...)
	merged.Extensions = appendUnique(merged.Extensions, overlay.Extensions...)
	merged.Shebangs = appendUnique(merged.Shebangs, overlay.Shebangs...)
	if len(overlay.Candidates) > 0 {
		merged.Candidates = cloneServerCandidates(overlay.Candidates)
	}
	return merged
}

func appendUnique(values []string, additions ...string) []string {
	out := slices.Clone(values)
	seen := make(map[string]bool, len(out)+len(additions))
	for _, value := range out {
		seen[value] = true
	}
	for _, value := range additions {
		if seen[value] {
			continue
		}
		out = append(out, value)
		seen[value] = true
	}
	return out
}

func normalizeExtension(ext string) string {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" {
		return ""
	}
	if strings.HasPrefix(ext, ".") {
		return ext
	}
	return "." + ext
}
