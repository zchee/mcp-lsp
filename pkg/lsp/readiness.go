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
	"context"
	"errors"
	"reflect"
	"slices"
	"time"
)

// Readiness reports how a [Stable] lookup concluded.
type Readiness uint8

const (
	// ReadinessStable means two consecutive attempts succeeded with results
	// that were equal after canonicalization.
	ReadinessStable Readiness = iota + 1
	// ReadinessExhausted means the attempt budget or the context ran out
	// before results stabilized.
	ReadinessExhausted
)

// String returns the agent-facing vocabulary for r.
func (r Readiness) String() string {
	switch r {
	case ReadinessStable:
		return "stable"
	case ReadinessExhausted:
		return "exhausted"
	default:
		return "unknown"
	}
}

// Default [StableConfig] bounds, generalizing the Attempts/RetryDelay retry
// idiom the domain-integration tests use against eventually-consistent
// language servers.
const (
	defaultStableAttempts = 10
	defaultStableDelay    = 250 * time.Millisecond
)

// StableConfig bounds a [Stable] lookup. Zero or negative fields fall back to
// the package defaults.
type StableConfig struct {
	Attempts int
	Delay    time.Duration
}

func (c StableConfig) withDefaults() StableConfig {
	if c.Attempts <= 0 {
		c.Attempts = defaultStableAttempts
	}
	if c.Delay <= 0 {
		c.Delay = defaultStableDelay
	}
	return c
}

// Stable re-invokes lookup until two consecutive attempts succeed with results
// that are equal after canonicalize, defending against language servers that
// return empty or partial results while still indexing. A lookup error is
// retried through and invalidates the comparison baseline, so stability is
// only ever declared across consecutive successes. canonicalize may sort its
// argument in place; it receives a fresh clone on every attempt and its output
// is what gets compared and returned, keeping results deterministic across
// servers that reorder responses. A nil canonicalize compares results as
// returned.
//
// Stable returns the canonicalized result of the last successful attempt. The
// [Readiness] is [ReadinessStable] when stability was reached and
// [ReadinessExhausted] when attempts or the context ran out first. The error
// is non-nil only when no attempt succeeded.
func Stable[T any](ctx context.Context, cfg StableConfig, canonicalize func([]T) []T, lookup func(context.Context) ([]T, error)) ([]T, Readiness, error) {
	cfg = cfg.withDefaults()
	if canonicalize == nil {
		canonicalize = func(in []T) []T { return in }
	}

	var (
		prev     []T
		havePrev bool
		lastErr  error
	)
	for attempt := range cfg.Attempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				if havePrev {
					return prev, ReadinessExhausted, nil
				}
				return nil, ReadinessExhausted, context.Cause(ctx)
			case <-time.After(cfg.Delay):
			}
		}

		result, err := lookup(ctx)
		if err != nil {
			if errors.Is(err, ErrUnsupported) {
				// Capability absence is immutable for the session's lifetime;
				// retrying cannot help.
				return nil, ReadinessExhausted, err
			}
			lastErr = err
			prev = nil
			havePrev = false
			continue
		}
		lastErr = nil

		current := canonicalize(slices.Clone(result))
		if havePrev && reflect.DeepEqual(prev, current) {
			return current, ReadinessStable, nil
		}
		prev = current
		havePrev = true
	}

	if !havePrev {
		return nil, ReadinessExhausted, lastErr
	}
	return prev, ReadinessExhausted, nil
}
