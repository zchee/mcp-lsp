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

	"go.lsp.dev/protocol"
)

func TestMessageText(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input protocol.InlayHintTooltip
		want  string
	}{
		"success: string arm": {
			input: protocol.String("boom"),
			want:  "boom",
		},
		"success: markup content arm": {
			input: &protocol.MarkupContent{Kind: protocol.MarkupKindMarkdown, Value: "**bold**"},
			want:  "**bold**",
		},
		"success: nil markup content pointer": {
			input: (*protocol.MarkupContent)(nil),
			want:  "",
		},
		"success: nil union": {
			input: nil,
			want:  "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := messageText(tt.input); got != tt.want {
				t.Errorf("messageText(%#v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCodeString(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input protocol.ProgressToken
		want  string
	}{
		"success: integer arm": {
			input: protocol.Integer(404),
			want:  "404",
		},
		"success: negative integer arm": {
			input: protocol.Integer(-7),
			want:  "-7",
		},
		"success: string arm": {
			input: protocol.String("E0001"),
			want:  "E0001",
		},
		"success: nil union": {
			input: nil,
			want:  "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := codeString(tt.input); got != tt.want {
				t.Errorf("codeString(%#v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSeverityString(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input protocol.DiagnosticSeverity
		want  string
	}{
		"success: error":         {input: protocol.DiagnosticSeverityError, want: "error"},
		"success: warning":       {input: protocol.DiagnosticSeverityWarning, want: "warning"},
		"success: information":   {input: protocol.DiagnosticSeverityInformation, want: "information"},
		"success: hint":          {input: protocol.DiagnosticSeverityHint, want: "hint"},
		"success: unset is zero": {input: 0, want: ""},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := severityString(tt.input); got != tt.want {
				t.Errorf("severityString(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestOptString(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		input protocol.Optional[string]
		want  string
	}{
		"success: present value": {
			input: protocol.NewOptional("gopls"),
			want:  "gopls",
		},
		"success: present empty string": {
			input: protocol.NewOptional(""),
			want:  "",
		},
		"success: absent": {
			input: protocol.Optional[string]{},
			want:  "",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := optString(tt.input); got != tt.want {
				t.Errorf("optString(%#v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
