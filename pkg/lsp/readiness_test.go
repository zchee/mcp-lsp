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
	"fmt"
	"slices"
	"testing"
	"time"

	gocmp "github.com/google/go-cmp/cmp"
)

func sortStrings(in []string) []string {
	slices.Sort(in)
	return in
}

func TestStable(t *testing.T) {
	t.Parallel()

	lookupErr := errors.New("server still indexing")

	tests := map[string]struct {
		cfg           StableConfig
		canonicalize  func([]string) []string
		results       [][]string
		errs          []error
		want          []string
		wantReadiness Readiness
		wantErr       error
	}{
		"success: stable after two equal results": {
			cfg:           StableConfig{Attempts: 5, Delay: time.Millisecond},
			canonicalize:  sortStrings,
			results:       [][]string{{"a", "b"}, {"a", "b"}},
			errs:          []error{nil, nil},
			want:          []string{"a", "b"},
			wantReadiness: ReadinessStable,
		},
		"success: order-shuffled results are stable after canonicalize": {
			cfg:           StableConfig{Attempts: 5, Delay: time.Millisecond},
			canonicalize:  sortStrings,
			results:       [][]string{{"b", "a"}, {"a", "b"}},
			errs:          []error{nil, nil},
			want:          []string{"a", "b"},
			wantReadiness: ReadinessStable,
		},
		"success: retries through transient error then stabilizes": {
			cfg:           StableConfig{Attempts: 5, Delay: time.Millisecond},
			canonicalize:  sortStrings,
			results:       [][]string{nil, {"a"}, {"a"}},
			errs:          []error{lookupErr, nil, nil},
			want:          []string{"a"},
			wantReadiness: ReadinessStable,
		},
		"success: error between successes invalidates the baseline": {
			cfg:           StableConfig{Attempts: 4, Delay: time.Millisecond},
			canonicalize:  sortStrings,
			results:       [][]string{{"a"}, nil, {"a"}, {"a"}},
			errs:          []error{nil, lookupErr, nil, nil},
			want:          []string{"a"},
			wantReadiness: ReadinessStable,
		},
		"success: two consecutive empty results are stable": {
			cfg:           StableConfig{Attempts: 5, Delay: time.Millisecond},
			canonicalize:  sortStrings,
			results:       [][]string{{}, {}},
			errs:          []error{nil, nil},
			want:          []string{},
			wantReadiness: ReadinessStable,
		},
		"success: nil canonicalize compares results as returned": {
			cfg:           StableConfig{Attempts: 5, Delay: time.Millisecond},
			results:       [][]string{{"a"}, {"a"}},
			errs:          []error{nil, nil},
			want:          []string{"a"},
			wantReadiness: ReadinessStable,
		},
		"error: changing results exhaust attempts": {
			cfg:           StableConfig{Attempts: 3, Delay: time.Millisecond},
			canonicalize:  sortStrings,
			results:       [][]string{{"a"}, {"a", "b"}, {"a", "b", "c"}},
			errs:          []error{nil, nil, nil},
			want:          []string{"a", "b", "c"},
			wantReadiness: ReadinessExhausted,
		},
		"error: no success returns the last error": {
			cfg:           StableConfig{Attempts: 3, Delay: time.Millisecond},
			canonicalize:  sortStrings,
			results:       [][]string{nil, nil, nil},
			errs:          []error{lookupErr, lookupErr, lookupErr},
			want:          nil,
			wantReadiness: ReadinessExhausted,
			wantErr:       lookupErr,
		},
		"error: unsupported capability fails fast without retrying": {
			cfg:           StableConfig{Attempts: 10, Delay: time.Hour},
			canonicalize:  sortStrings,
			results:       [][]string{nil},
			errs:          []error{fmt.Errorf("references request: %w", ErrUnsupported)},
			want:          nil,
			wantReadiness: ReadinessExhausted,
			wantErr:       ErrUnsupported,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			call := 0
			lookup := func(context.Context) ([]string, error) {
				if call >= len(tt.results) {
					t.Fatalf("lookup called %d times, only %d results configured", call+1, len(tt.results))
				}
				result, err := tt.results[call], tt.errs[call]
				call++
				return result, err
			}

			got, readiness, err := Stable(t.Context(), tt.cfg, tt.canonicalize, lookup)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Stable error = %v, want %v", err, tt.wantErr)
			}
			if readiness != tt.wantReadiness {
				t.Errorf("Stable readiness = %v, want %v", readiness, tt.wantReadiness)
			}
			if diff := gocmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Stable result mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStableContextCanceled(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		results       [][]string
		errs          []error
		want          []string
		wantReadiness Readiness
		wantErr       error
	}{
		"success: cancellation after a success returns the partial result": {
			results:       [][]string{{"a"}},
			errs:          []error{nil},
			want:          []string{"a"},
			wantReadiness: ReadinessExhausted,
		},
		"error: cancellation without a success returns the context cause": {
			results:       [][]string{nil},
			errs:          []error{errors.New("first attempt failed")},
			want:          nil,
			wantReadiness: ReadinessExhausted,
			wantErr:       context.Canceled,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			call := 0
			lookup := func(context.Context) ([]string, error) {
				result, err := tt.results[call], tt.errs[call]
				call++
				cancel()
				return result, err
			}

			got, readiness, err := Stable(ctx, StableConfig{Attempts: 5, Delay: time.Hour}, sortStrings, lookup)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Stable error = %v, want %v", err, tt.wantErr)
			}
			if readiness != tt.wantReadiness {
				t.Errorf("Stable readiness = %v, want %v", readiness, tt.wantReadiness)
			}
			if diff := gocmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Stable result mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestStableDefaults(t *testing.T) {
	t.Parallel()

	got := StableConfig{}.withDefaults()
	want := StableConfig{Attempts: defaultStableAttempts, Delay: defaultStableDelay}
	if got != want {
		t.Errorf("withDefaults() = %+v, want %+v", got, want)
	}
}

func TestReadinessString(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		readiness Readiness
		want      string
	}{
		"success: stable":     {readiness: ReadinessStable, want: "stable"},
		"success: exhausted":  {readiness: ReadinessExhausted, want: "exhausted"},
		"success: zero value": {readiness: 0, want: "unknown"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := tt.readiness.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}
