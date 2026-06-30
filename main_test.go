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
			},
		},
		"lsp command and child args after delimiter": {
			args: []string{"-workspace", workspace, "-log-level", "debug", "-lsp", "custom-gopls", "--", "-remote=auto", "--stdio"},
			want: cliConfig{
				workspace:  workspace,
				logLevel:   "debug",
				lspCommand: "custom-gopls",
				lspArgs:    []string{"-remote=auto", "--stdio"},
				lang:       "go",
			},
		},
		"lsp command without child args": {
			args: []string{"-lsp", "custom-gopls"},
			want: cliConfig{
				workspace:  workspace,
				logLevel:   "info",
				lspCommand: "custom-gopls",
				lang:       "go",
			},
		},
		"basedpyright command infers python": {
			args: []string{"-lsp", "basedpyright-langserver", "--", "--stdio"},
			want: cliConfig{
				workspace:  workspace,
				logLevel:   "info",
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
				lspCommand: "custom-server",
				lang:       "python",
			},
		},
		"empty delimiter is a no-op without lsp command": {
			args: []string{"--"},
			want: cliConfig{
				workspace: workspace,
				logLevel:  "info",
			},
		},
		"version flag remains independent": {
			args: []string{"-version"},
			want: cliConfig{
				workspace:   workspace,
				logLevel:    "info",
				showVersion: true,
			},
		},
		"version flag ignores server-only arguments": {
			args: []string{"-version", "positional", "--", "--stdio"},
			want: cliConfig{
				workspace:   workspace,
				logLevel:    "info",
				showVersion: true,
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
