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

package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
)

func TestParseCLI(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	tests := map[string]struct {
		args []string
		want cliConfig
	}{
		"default values": {
			want: cliConfig{
				workspace: workspace,
				logLevel:  "info",
				discover:  true,
			},
		},
		"lsp command and child args after delimiter": {
			args: []string{"-workspace", workspace, "-log-level", "debug", "-lsp", "gopls", "--", "-remote=auto", "--stdio"},
			want: cliConfig{
				workspace:  workspace,
				logLevel:   "debug",
				discover:   true,
				lspCommand: "gopls",
				lspArgs:    []string{"-remote=auto", "--stdio"},
				lang:       "go",
			},
		},
		"lsp command without child args": {
			args: []string{"-lsp", "gopls"},
			want: cliConfig{
				workspace:  workspace,
				logLevel:   "info",
				discover:   true,
				lspCommand: "gopls",
				lang:       "go",
			},
		},
		"basedpyright command infers python": {
			args: []string{"-lsp", "basedpyright-langserver", "--", "--stdio"},
			want: cliConfig{
				workspace:  workspace,
				logLevel:   "info",
				discover:   true,
				lspCommand: "basedpyright-langserver",
				lspArgs:    []string{"--stdio"},
				lang:       "python",
			},
		},
		"explicit language alias canonicalizes for lsp command": {
			args: []string{"-language", "basedpyright", "-lsp", "custom-server"},
			want: cliConfig{
				workspace:  workspace,
				logLevel:   "info",
				discover:   true,
				lspCommand: "custom-server",
				lang:       "python",
			},
		},
		"empty delimiter is a no-op without lsp command": {
			args: []string{"--"},
			want: cliConfig{
				workspace: workspace,
				logLevel:  "info",
				discover:  true,
			},
		},
		"version flag remains independent": {
			args: []string{"-version"},
			want: cliConfig{
				workspace:   workspace,
				logLevel:    "info",
				discover:    true,
				showVersion: true,
			},
		},
		"version flag ignores server-only arguments": {
			args: []string{"-version", "positional", "--", "--stdio"},
			want: cliConfig{
				workspace:   workspace,
				logLevel:    "info",
				discover:    true,
				showVersion: true,
			},
		},
		"discover can be disabled": {
			args: []string{"-discover=false"},
			want: cliConfig{
				workspace: workspace,
				logLevel:  "info",
				discover:  false,
			},
		},
		"explicit config path": {
			args: []string{"-config", "servers.json"},
			want: cliConfig{
				workspace:  workspace,
				logLevel:   "info",
				configPath: "servers.json",
				discover:   true,
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := parseCLI(slices.Clone(tt.args), workspace)
			if err != nil {
				t.Fatalf("parseCLI() error = %v", err)
			}
			if diff := gocmp.Diff(tt.want, got, gocmp.AllowUnexported(cliConfig{})); diff != "" {
				t.Errorf("parseCLI() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestLoadRuntimeRegistry(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	configPath := filepath.Join(workspace, ".mcp-lsp.json")
	if err := os.WriteFile(configPath, []byte(`{
		"servers": {
			"python": {
				"command": "basedpyright-langserver",
				"args": ["--stdio"],
				"languageId": "python",
				"extensions": [".py", ".pyi"],
				"aliases": ["py", "basedpyright"]
			}
		}
	}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	tests := map[string]struct {
		cfg       cliConfig
		wantLangs []string
		wantCfg   map[string]string
	}{
		"empty when discovery disabled and no config": {
			cfg: cliConfig{
				workspace: t.TempDir(),
				discover:  false,
			},
			wantLangs: []string{},
		},
		"workspace config is loaded": {
			cfg: cliConfig{
				workspace: workspace,
				discover:  false,
			},
			wantLangs: []string{"python"},
			wantCfg: map[string]string{
				"python": "basedpyright-langserver",
			},
		},
		"known cli override infers python without language": {
			cfg: cliConfig{
				workspace:  t.TempDir(),
				discover:   false,
				lspCommand: "basedpyright-langserver",
				lspArgs:    []string{"--stdio"},
				lang:       "python",
			},
			wantLangs: []string{"python"},
			wantCfg: map[string]string{
				"python": "basedpyright-langserver",
			},
		},
		"cli override wins over config": {
			cfg: cliConfig{
				workspace:  workspace,
				discover:   false,
				lspCommand: "pyright-langserver",
				lspArgs:    []string{"--stdio"},
				lang:       "python",
			},
			wantLangs: []string{"python"},
			wantCfg: map[string]string{
				"python": "pyright-langserver",
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			registry, _, err := loadRuntimeRegistry(&tt.cfg)
			if err != nil {
				t.Fatalf("loadRuntimeRegistry() error = %v", err)
			}
			if diff := gocmp.Diff(tt.wantLangs, registry.ConfiguredLanguages()); diff != "" {
				t.Fatalf("configured languages mismatch (-want +got):\n%s", diff)
			}
			for language, command := range tt.wantCfg {
				serverCfg, ok := registry.ServerConfig(language)
				if !ok {
					t.Fatalf("ServerConfig(%q) not found", language)
				}
				if serverCfg.Command != command {
					t.Fatalf("ServerConfig(%q).Command = %q, want %q", language, serverCfg.Command, command)
				}
			}
		})
	}
}

func TestLoadRuntimeRegistryExplicitMissingConfigErrors(t *testing.T) {
	t.Parallel()

	cfg := cliConfig{
		workspace:  t.TempDir(),
		configPath: filepath.Join(t.TempDir(), "missing.json"),
		discover:   false,
	}
	_, _, err := loadRuntimeRegistry(&cfg)
	if err == nil || !strings.Contains(err.Error(), "read config") {
		t.Fatalf("loadRuntimeRegistry() error = %v, want read config error", err)
	}
}

func TestLoadRuntimeRegistryRejectsDuplicateCanonicalConfigLanguages(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	configPath := filepath.Join(workspace, ".mcp-lsp.json")
	if err := os.WriteFile(configPath, []byte(`{
		"servers": {
			"python": {
				"command": "pyright-langserver",
				"args": ["--stdio"],
				"languageId": "python"
			},
			"py": {
				"command": "basedpyright-langserver",
				"args": ["--stdio"],
				"languageId": "python"
			}
		}
	}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg := cliConfig{
		workspace: workspace,
		discover:  false,
	}
	_, _, err := loadRuntimeRegistry(&cfg)
	if err == nil || !strings.Contains(err.Error(), "duplicate config language") {
		t.Fatalf("loadRuntimeRegistry() error = %v, want duplicate config language error", err)
	}
}

func TestParseCLIRejectsInvalidArgs(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		args        []string
		wantContain string
	}{
		"lsp args without command": {
			args:        []string{"--", "--stdio"},
			wantContain: "-lsp",
		},
		"positional before delimiter": {
			args:        []string{"main.go"},
			wantContain: "unexpected positional arguments before --",
		},
		"positional after lsp command before delimiter": {
			args:        []string{"-lsp", "gopls", "main.go"},
			wantContain: "unexpected positional arguments before --",
		},
		"empty lsp command": {
			args:        []string{"-lsp", ""},
			wantContain: "lsp command is required",
		},
		"custom lsp command without inferable language": {
			args:        []string{"-lsp", "custom-server"},
			wantContain: "language is required",
		},
		"wrapper lsp command without explicit language": {
			args:        []string{"-lsp", "custom-gopls"},
			wantContain: "language is required",
		},
		"empty language": {
			args:        []string{"-language", "", "-lsp", "gopls"},
			wantContain: "language is required",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := parseCLI(slices.Clone(tt.args), t.TempDir())
			if err == nil {
				t.Fatal("parseCLI() error = nil")
			}
			if !strings.Contains(err.Error(), tt.wantContain) {
				t.Fatalf("parseCLI() error = %q, want contains %q", err, tt.wantContain)
			}
		})
	}
}

func TestSplitLSPArgs(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		args             []string
		wantFlagArgs     []string
		wantLSPArgs      []string
		wantHasDelimiter bool
	}{
		"no delimiter": {
			args:         []string{"-workspace", "/repo"},
			wantFlagArgs: []string{"-workspace", "/repo"},
		},
		"first delimiter separates child args": {
			args:             []string{"-lsp", "gopls", "--", "-remote=auto", "--stdio"},
			wantFlagArgs:     []string{"-lsp", "gopls"},
			wantLSPArgs:      []string{"-remote=auto", "--stdio"},
			wantHasDelimiter: true,
		},
		"second delimiter belongs to child args": {
			args:             []string{"-lsp", "gopls", "--", "--", "child"},
			wantFlagArgs:     []string{"-lsp", "gopls"},
			wantLSPArgs:      []string{"--", "child"},
			wantHasDelimiter: true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			gotFlagArgs, gotLSPArgs, gotHasDelimiter := splitLSPArgs(slices.Clone(tt.args))
			if diff := gocmp.Diff(tt.wantFlagArgs, gotFlagArgs); diff != "" {
				t.Errorf("flag args mismatch (-want +got):\n%s", diff)
			}
			if diff := gocmp.Diff(tt.wantLSPArgs, gotLSPArgs); diff != "" {
				t.Errorf("lsp args mismatch (-want +got):\n%s", diff)
			}
			if gotHasDelimiter != tt.wantHasDelimiter {
				t.Errorf("hasDelimiter = %t, want %t", gotHasDelimiter, tt.wantHasDelimiter)
			}
		})
	}
}
