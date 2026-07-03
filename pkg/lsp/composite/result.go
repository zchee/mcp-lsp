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

package composite

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

// Meta is the metadata block every flagship composite carries. Readiness and
// StopReason describe how the composite concluded; EpicenterTextHash is the
// SHA-256 of the epicenter text the composite threaded through all its legs, so
// a caller can detect that a concurrent on-disk edit changed the file out from
// under the analysis; CapabilitiesUsed/Missing report which requested
// capabilities the server advertised.
type Meta struct {
	Readiness           string
	StopReason          string
	EpicenterTextHash   string
	CapabilitiesUsed    []Capability
	CapabilitiesMissing []Capability
}

// hashText returns the hex-encoded SHA-256 of text, the epicenter-text
// fingerprint shared by every composite.
func hashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// readinessString maps an lsp.Readiness to the agent-facing vocabulary the
// composites emit: a stable lookup is "stable" and an exhausted one is
// "notReady", matching the Meta.Readiness field.
func readinessString(r lsp.Readiness) string {
	switch r {
	case lsp.ReadinessStable:
		return "stable"
	case lsp.ReadinessExhausted:
		return "notReady"
	default:
		return "unknown"
	}
}
