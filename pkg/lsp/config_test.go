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
	"os/exec"
	"strings"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
	"go.lsp.dev/protocol"
)

func TestDefaultCatalogContainsLanguageMetadataButNoActiveServers(t *testing.T) {
	t.Parallel()

	specs := DefaultCatalog()
	registry, err := NewRegistry(specs, nil)
	if err != nil {
		t.Fatalf("NewRegistry(DefaultCatalog(), nil): %v", err)
	}
	if got := registry.ConfiguredLanguages(); len(got) != 0 {
		t.Fatalf("ConfiguredLanguages() = %v, want no active servers", got)
	}
	if _, ok := registry.ServerConfig("go"); ok {
		t.Fatal("ServerConfig(\"go\") unexpectedly returned an active server")
	}

	tests := map[string]struct {
		language   string
		aliases    []string
		extensions []string
		languageID protocol.LanguageKind
	}{
		"go": {
			language:   "go",
			extensions: []string{".go"},
			languageID: protocol.LanguageKindGo,
		},
		"python": {
			language:   "python",
			aliases:    []string{"py", "pyright", "basedpyright"},
			extensions: []string{".py", ".pyi"},
			languageID: protocol.LanguageKindPython,
		},
		"rust": {
			language:   "rust",
			extensions: []string{".rs"},
			languageID: protocol.LanguageKindRust,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			spec, ok := registry.LanguageSpec(tt.language)
			if !ok {
				t.Fatalf("LanguageSpec(%q) not found", tt.language)
			}
			if spec.Language != tt.language || spec.LanguageID != tt.languageID {
				t.Fatalf("LanguageSpec(%q) = %+v", tt.language, spec)
			}
			if diff := gocmp.Diff(tt.aliases, spec.Aliases); diff != "" {
				t.Fatalf("aliases mismatch (-want +got):\n%s", diff)
			}
			if diff := gocmp.Diff(tt.extensions, spec.Extensions); diff != "" {
				t.Fatalf("extensions mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCanonicalLanguage(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input string
		want  string
	}{
		"basedpyright maps to python": {
			input: "basedpyright",
			want:  "python",
		},
		"py maps to python": {
			input: "py",
			want:  "python",
		},
		"pyright maps to python": {
			input: "pyright",
			want:  "python",
		},
		"preserves other normalized languages": {
			input: " RUST ",
			want:  "rust",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := CanonicalLanguage(tt.input); got != tt.want {
				t.Fatalf("CanonicalLanguage(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestInferLanguageFromCommand(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		command string
		want    string
		wantOK  bool
	}{
		"basedpyright langserver": {
			command: "basedpyright-langserver",
			want:    "python",
			wantOK:  true,
		},
		"pyright langserver path": {
			command: "/opt/bin/pyright-langserver",
			want:    "python",
			wantOK:  true,
		},
		"gopls command": {
			command: "gopls",
			want:    "go",
			wantOK:  true,
		},
		"misleading wrapper does not infer": {
			command: "custom-gopls",
		},
		"unknown command": {
			command: "language-server",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, ok := InferLanguageFromCommand(tt.command)
			if ok != tt.wantOK {
				t.Fatalf("InferLanguageFromCommand(%q) ok = %t, want %t", tt.command, ok, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("InferLanguageFromCommand(%q) = %q, want %q", tt.command, got, tt.want)
			}
		})
	}
}

func TestDiscoverServerConfigsUsesLookPathAndCandidateOrder(t *testing.T) {
	t.Parallel()

	paths := map[string]string{
		"basedpyright-langserver": "/bin/basedpyright-langserver",
		"pyright-langserver":      "/bin/pyright-langserver",
		"rust-analyzer":           "/bin/rust-analyzer",
	}
	lookPath := func(command string) (string, error) {
		if path, ok := paths[command]; ok {
			return path, nil
		}
		return "", exec.ErrNotFound
	}

	got := DiscoverServerConfigs(DefaultCatalog(), lookPath)
	want := map[string]ServerConfig{
		"python": {
			Command:    "/bin/basedpyright-langserver",
			Args:       []string{"--stdio"},
			LanguageID: protocol.LanguageKindPython,
		},
		"rust": {
			Command:    "/bin/rust-analyzer",
			LanguageID: protocol.LanguageKindRust,
		},
	}
	if diff := gocmp.Diff(want, got); diff != "" {
		t.Fatalf("DiscoverServerConfigs mismatch (-want +got):\n%s", diff)
	}
	if _, ok := got["go"]; ok {
		t.Fatal("gopls became active without LookPath success")
	}
}

func TestRegistryCanonicalizationCloningAndFileInference(t *testing.T) {
	t.Parallel()

	cfg := map[string]ServerConfig{
		"py": {
			Command:    "pyright-langserver",
			Args:       []string{"--stdio"},
			LanguageID: protocol.LanguageKindPython,
		},
	}
	registry, err := NewRegistry(DefaultCatalog(), cfg)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	cfg["py"] = ServerConfig{}

	serverCfg, ok := registry.ServerConfig("basedpyright")
	if !ok {
		t.Fatal("ServerConfig(basedpyright) not found")
	}
	serverCfg.Args[0] = "--mutated"
	again, _ := registry.ServerConfig("python")
	if diff := gocmp.Diff([]string{"--stdio"}, again.Args); diff != "" {
		t.Fatalf("ServerConfig args were not cloned (-want +got):\n%s", diff)
	}
	if got, ok := registry.LanguageForFile("main.py", ""); !ok || got != "python" {
		t.Fatalf("LanguageForFile(main.py) = %q/%t, want python/true", got, ok)
	}
	if got, ok := registry.LanguageForFile("script", "#!/usr/bin/env python\n"); !ok || got != "python" {
		t.Fatalf("LanguageForFile(shebang) = %q/%t, want python/true", got, ok)
	}
}

func TestRegistryRejectsConflictingAliasesAndExtensions(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		specs []LanguageSpec
		want  string
	}{
		"alias conflict": {
			specs: []LanguageSpec{
				{Language: "one", Aliases: []string{"shared"}},
				{Language: "two", Aliases: []string{"shared"}},
			},
			want: "alias",
		},
		"extension conflict": {
			specs: []LanguageSpec{
				{Language: "one", Extensions: []string{".x"}},
				{Language: "two", Extensions: []string{".x"}},
			},
			want: "extension",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := NewRegistry(tt.specs, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("NewRegistry error = %v, want contains %q", err, tt.want)
			}
		})
	}
}

func TestRegistrySupportsCustomConfiguredLanguage(t *testing.T) {
	t.Parallel()

	registry, err := NewRegistry(nil, map[string]ServerConfig{
		"zig": {Command: "zls"},
	})
	if err != nil {
		t.Fatalf("NewRegistry custom language: %v", err)
	}
	cfg, ok := registry.ServerConfig("zig")
	if !ok {
		t.Fatal("ServerConfig(zig) not found")
	}
	if got := fmt.Sprint(cfg.LanguageID); got != "zig" {
		t.Fatalf("custom language ID = %q, want zig", got)
	}
}

func TestRegistryMergesDuplicateCanonicalSpecs(t *testing.T) {
	t.Parallel()

	// Two specs canonicalize to "go": the second overlays additional identity
	// metadata that must union with the base instead of replacing it.
	registry, err := NewRegistry([]LanguageSpec{
		{
			Language:   languageGo,
			LanguageID: protocol.LanguageKindGo,
			Aliases:    []string{"golang"},
			Extensions: []string{".go"},
		},
		{
			Language:   "Go",
			Aliases:    []string{"golang", "go-lang"},
			Extensions: []string{".gohtml"},
			Shebangs:   []string{"gorun"},
			Candidates: []ServerCandidate{{Command: "gopls"}},
		},
	}, nil)
	if err != nil {
		t.Fatalf("NewRegistry merge specs: %v", err)
	}

	spec, ok := registry.LanguageSpec(languageGo)
	if !ok {
		t.Fatalf("LanguageSpec(%q) not found", languageGo)
	}

	wantAliases := []string{"golang", "go-lang"}
	if diff := gocmp.Diff(wantAliases, spec.Aliases); diff != "" {
		t.Errorf("merged aliases mismatch (-want +got):\n%s", diff)
	}
	wantExtensions := []string{".go", ".gohtml"}
	if diff := gocmp.Diff(wantExtensions, spec.Extensions); diff != "" {
		t.Errorf("merged extensions mismatch (-want +got):\n%s", diff)
	}
	wantShebangs := []string{"gorun"}
	if diff := gocmp.Diff(wantShebangs, spec.Shebangs); diff != "" {
		t.Errorf("merged shebangs mismatch (-want +got):\n%s", diff)
	}
	wantCandidates := []ServerCandidate{{Command: "gopls"}}
	if diff := gocmp.Diff(wantCandidates, spec.Candidates); diff != "" {
		t.Errorf("merged candidates mismatch (-want +got):\n%s", diff)
	}

	for _, alias := range []string{"golang", "go-lang"} {
		if got, ok := registry.CanonicalLanguage(alias); !ok || got != languageGo {
			t.Errorf("CanonicalLanguage(%q) = %q/%t, want %q/true", alias, got, ok, languageGo)
		}
	}
	if got, ok := registry.LanguageForFile("page.gohtml", ""); !ok || got != languageGo {
		t.Errorf("LanguageForFile(page.gohtml) = %q/%t, want %q/true", got, ok, languageGo)
	}
}
