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
	"strings"
	"testing"
)

func TestLanguageResolverResolveFileLanguage(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		file      string
		text      string
		explicit  string
		resolver  languageResolver
		want      string
		wantError string
	}{
		"go extension": {
			file:     "main.go",
			resolver: testResolver(t, "go", "python", "rust"),
			want:     "go",
		},
		"python extension": {
			file:     "main.py",
			resolver: testResolver(t, "go", "python", "rust"),
			want:     "python",
		},
		"python stub extension": {
			file:     "types.pyi",
			resolver: testResolver(t, "python"),
			want:     "python",
		},
		"rust extension": {
			file:     "main.rs",
			resolver: testResolver(t, "go", "python", "rust"),
			want:     "rust",
		},
		"python shebang": {
			file:     "script",
			text:     "#!/usr/bin/env python\nprint('ok')\n",
			resolver: testResolver(t, "python"),
			want:     "python",
		},
		"explicit language overrides extension": {
			file:     "generated.txt",
			explicit: "basedpyright",
			resolver: testResolver(t, "python"),
			want:     "python",
		},
		"known extension without configured server": {
			file:      "main.py",
			resolver:  testResolver(t, "go"),
			wantError: "no language server configured for python files",
		},
		"unknown extension with multiple configured servers": {
			file:      "README.md",
			resolver:  testResolver(t, "go", "python"),
			wantError: "cannot infer language",
		},
		"unknown explicit language": {
			file:      "main.go",
			explicit:  "unknown",
			resolver:  testResolver(t, "go"),
			wantError: "unknown language",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.resolver.ResolveFileLanguage(tt.file, tt.text, tt.explicit)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("ResolveFileLanguage() error = %v, want contains %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveFileLanguage() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ResolveFileLanguage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLanguageResolverResolveToolLanguage(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		explicit  string
		resolver  languageResolver
		want      string
		wantError string
	}{
		"explicit alias": {
			explicit: "py",
			resolver: testResolver(t, "python"),
			want:     "python",
		},
		"single configured fallback": {
			resolver: testResolver(t, "rust"),
			want:     "rust",
		},
		"multiple configured requires language": {
			resolver:  testResolver(t, "go", "python"),
			wantError: "language is required",
		},
		"no configured requires language": {
			resolver:  testResolver(t),
			wantError: "no language servers configured",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.resolver.ResolveToolLanguage(tt.explicit)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("ResolveToolLanguage() error = %v, want contains %q", err, tt.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveToolLanguage() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ResolveToolLanguage() = %q, want %q", got, tt.want)
			}
		})
	}
}
