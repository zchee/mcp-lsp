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
	"strconv"

	"go.lsp.dev/protocol"
)

// messageText flattens a diagnostic message union (String | *MarkupContent)
// into plain text. A nil message yields the empty string.
func messageText(msg protocol.InlayHintTooltip) string {
	switch v := msg.(type) {
	case protocol.String:
		return string(v)
	case *protocol.MarkupContent:
		if v == nil {
			return ""
		}

		return v.Value
	default:
		return ""
	}
}

// codeString flattens a diagnostic code union (Integer | String) into its
// string form. A nil code yields the empty string.
func codeString(code protocol.ProgressToken) string {
	switch v := code.(type) {
	case protocol.Integer:
		return strconv.FormatInt(int64(v), 10)
	case protocol.String:
		return string(v)
	default:
		return ""
	}
}

// severityString maps an LSP diagnostic severity to a human-readable label. An
// unset severity (0) yields the empty string.
func severityString(sev protocol.DiagnosticSeverity) string {
	switch sev {
	case protocol.DiagnosticSeverityError:
		return "error"
	case protocol.DiagnosticSeverityWarning:
		return "warning"
	case protocol.DiagnosticSeverityInformation:
		return "information"
	case protocol.DiagnosticSeverityHint:
		return "hint"
	default:
		return ""
	}
}

// optString returns the value of an optional string property, or the empty
// string when it is absent.
func optString(opt protocol.Optional[string]) string {
	v, _ := opt.Get()

	return v
}
