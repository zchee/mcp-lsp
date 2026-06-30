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
	"path/filepath"
	"slices"
	"strings"

	"go.lsp.dev/protocol"
)

// ServerCandidate describes a known executable shape that can be discovered on
// PATH for one language. Candidates are metadata, not active server defaults.
type ServerCandidate struct {
	// Command is the executable name to resolve on PATH.
	Command string

	// Args are the arguments used when the candidate is selected.
	Args []string
}

// LanguageSpec describes stable language identity metadata used for
// canonicalization, file inference, LSP document language IDs, and discovery.
// It does not mean a server is configured or runnable.
type LanguageSpec struct {
	// Language is the canonical language key accepted by MCP tool inputs.
	Language string

	// LanguageID is the LSP text document language kind for the language.
	LanguageID protocol.LanguageKind

	// Aliases are alternate tool input identifiers for the language.
	Aliases []string

	// Extensions are file extensions owned by the language.
	Extensions []string

	// Shebangs are lower-case shebang substrings that identify the language.
	Shebangs []string

	// Candidates are ordered PATH discovery candidates for the language.
	Candidates []ServerCandidate
}

var builtInLanguageSpecs = []LanguageSpec{
	{
		Language:   languageGo,
		LanguageID: protocol.LanguageKindGo,
		Extensions: []string{".go"},
		Candidates: []ServerCandidate{{Command: "gopls"}},
	},
	{
		Language:   languagePython,
		LanguageID: protocol.LanguageKindPython,
		Aliases:    []string{"py", "pyright", "basedpyright"},
		Extensions: []string{".py", ".pyi"},
		Shebangs:   []string{"python"},
		Candidates: []ServerCandidate{
			{Command: "basedpyright-langserver", Args: []string{"--stdio"}},
			{Command: "pyright-langserver", Args: []string{"--stdio"}},
		},
	},
	{
		Language:   languageRust,
		LanguageID: protocol.LanguageKindRust,
		Extensions: []string{".rs"},
		Candidates: []ServerCandidate{{Command: "rust-analyzer"}},
	},
}

// DefaultCatalog returns the built-in language identity and discovery metadata.
// The returned specs are defensive copies and do not configure active servers.
func DefaultCatalog() []LanguageSpec {
	return cloneLanguageSpecs(builtInLanguageSpecs)
}

// CanonicalLanguage returns the built-in canonical language key for a
// user-supplied language identifier. Unknown identifiers are normalized and
// returned unchanged.
func CanonicalLanguage(lang string) string {
	registry, err := NewRegistry(DefaultCatalog(), nil)
	if err != nil {
		return normalizeLanguage(lang)
	}
	canonical, ok := registry.CanonicalLanguage(lang)
	if !ok {
		return normalizeLanguage(lang)
	}
	return canonical
}

// InferLanguageFromCommand returns the built-in canonical language served by a
// known language-server command.
func InferLanguageFromCommand(command string) (string, bool) {
	name := normalizedCommandBase(command)
	for i := range builtInLanguageSpecs {
		spec := &builtInLanguageSpecs[i]
		for _, candidate := range spec.Candidates {
			candidateName := normalizedCommandBase(candidate.Command)
			if name == candidateName {
				return spec.Language, true
			}
		}
	}
	return "", false
}

func normalizedCommandBase(command string) string {
	name := strings.ToLower(filepath.Base(command))
	return strings.TrimSuffix(name, ".exe")
}

func cloneLanguageSpec(spec *LanguageSpec) LanguageSpec {
	clone := *spec
	clone.Aliases = slices.Clone(spec.Aliases)
	clone.Extensions = slices.Clone(spec.Extensions)
	clone.Shebangs = slices.Clone(spec.Shebangs)
	clone.Candidates = cloneServerCandidates(spec.Candidates)
	return clone
}

func cloneLanguageSpecs(specs []LanguageSpec) []LanguageSpec {
	out := make([]LanguageSpec, 0, len(specs))
	for i := range specs {
		out = append(out, cloneLanguageSpec(&specs[i]))
	}
	return out
}

func cloneServerCandidate(candidate ServerCandidate) ServerCandidate {
	candidate.Args = slices.Clone(candidate.Args)
	return candidate
}

func cloneServerCandidates(candidates []ServerCandidate) []ServerCandidate {
	out := make([]ServerCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, cloneServerCandidate(candidate))
	}
	return out
}
