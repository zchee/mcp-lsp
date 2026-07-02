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
	"errors"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

// LegStatus reports the outcome of one leg of a composite so an agent can tell
// a capability gap, an indexing race, and a genuinely empty result apart
// instead of reading every empty payload as an authoritative zero.
type LegStatus uint8

const (
	// StatusOK means the leg returned data.
	StatusOK LegStatus = iota + 1
	// StatusEmpty means the leg completed and the server genuinely returned
	// nothing (only trustworthy once readiness-gated).
	StatusEmpty
	// StatusUnsupported means the server does not advertise the capability
	// this leg needs; it is distinct from empty.
	StatusUnsupported
	// StatusTruncated means the leg hit a budget cap and its data is partial.
	StatusTruncated
	// StatusError means the leg failed for a reason other than an unsupported
	// capability.
	StatusError
	// StatusNotReady means the leg could not be trusted within its readiness
	// budget, e.g. the server was still indexing.
	StatusNotReady
)

// String returns the agent-facing name of the status.
func (s LegStatus) String() string {
	switch s {
	case StatusOK:
		return "ok"
	case StatusEmpty:
		return "empty"
	case StatusUnsupported:
		return "unsupported"
	case StatusTruncated:
		return "truncated"
	case StatusError:
		return "error"
	case StatusNotReady:
		return "notReady"
	default:
		return "unknown"
	}
}

// StopReason records why a composite stopped, disambiguating a deadline exit
// from an instability exit so notReady is never ambiguous. When conditions
// co-occur the precedence is exhausted > deadline > budget > stable.
type StopReason uint8

const (
	// StopStable means the composite finished with everything it sought.
	StopStable StopReason = iota + 1
	// StopDeadline means the fan-out phase deadline cut the composite short.
	StopDeadline
	// StopExhausted means a readiness gate ran out of attempts (or a push
	// diagnostics settle timed out) before results stabilized.
	StopExhausted
	// StopBudget means at least one collection cap fired; the per-leg
	// truncated markers identify which.
	StopBudget
)

// String returns the agent-facing name of the stop reason.
func (r StopReason) String() string {
	switch r {
	case StopStable:
		return "stable"
	case StopDeadline:
		return "deadline"
	case StopExhausted:
		return "exhausted"
	case StopBudget:
		return "budget"
	default:
		return "unknown"
	}
}

// Leg is one leg of a composite result: its status, the data it produced, and
// a human/agent-readable note explaining any non-ok status.
type Leg[T any] struct {
	Status LegStatus
	Data   T
	Note   string
}

// LegFrom classifies a lookup that returns a slice and an error into a Leg.
// This is the single place capability errors are interpreted: an error
// wrapping [lsp.ErrUnsupported] becomes StatusUnsupported, any other error
// becomes StatusError, a nil error with no data becomes StatusEmpty, and a nil
// error with data becomes StatusOK. Nothing else in the package inspects error
// text to decide capability support.
func LegFrom[T any](data []T, err error) Leg[[]T] {
	if err != nil {
		if errors.Is(err, lsp.ErrUnsupported) {
			return Leg[[]T]{Status: StatusUnsupported, Note: err.Error()}
		}
		return Leg[[]T]{Status: StatusError, Note: err.Error()}
	}
	if len(data) == 0 {
		return Leg[[]T]{Status: StatusEmpty, Data: data}
	}
	return Leg[[]T]{Status: StatusOK, Data: data}
}
