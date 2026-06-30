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
	"testing"

	gocmp "github.com/google/go-cmp/cmp"
	"go.lsp.dev/protocol"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	want := map[string]ServerConfig{
		"go": {
			Command:    "gopls",
			Args:       nil,
			LanguageID: protocol.LanguageKindGo,
		},
		"python": {
			Command:    "pyright-langserver",
			Args:       []string{"--stdio"},
			LanguageID: protocol.LanguageKindPython,
		},
		"rust": {
			Command:    "rust-analyzer",
			Args:       nil,
			LanguageID: protocol.LanguageKindRust,
		},
	}

	got := DefaultConfig()
	if diff := gocmp.Diff(want, got); diff != "" {
		t.Errorf("DefaultConfig() mismatch (-want +got):\n%s", diff)
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
		"gopls wrapper": {
			command: "custom-gopls",
			want:    "go",
			wantOK:  true,
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
