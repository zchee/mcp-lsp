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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zchee/mcp-lsp/pkg/lsp"
)

func TestLegStatusString(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		status LegStatus
		want   string
	}{
		"success: ok":          {status: StatusOK, want: "ok"},
		"success: empty":       {status: StatusEmpty, want: "empty"},
		"success: unsupported": {status: StatusUnsupported, want: "unsupported"},
		"success: truncated":   {status: StatusTruncated, want: "truncated"},
		"success: error":       {status: StatusError, want: "error"},
		"success: notReady":    {status: StatusNotReady, want: "notReady"},
		"success: zero value":  {status: 0, want: "unknown"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := tt.status.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStopReasonString(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		reason StopReason
		want   string
	}{
		"success: stable":     {reason: StopStable, want: "stable"},
		"success: deadline":   {reason: StopDeadline, want: "deadline"},
		"success: exhausted":  {reason: StopExhausted, want: "exhausted"},
		"success: budget":     {reason: StopBudget, want: "budget"},
		"success: zero value": {reason: 0, want: "unknown"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := tt.reason.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLegFrom(t *testing.T) {
	t.Parallel()

	unsupported := fmt.Errorf("references request: %w", lsp.ErrUnsupported)
	failure := errors.New("server unavailable")

	tests := map[string]struct {
		data       []string
		err        error
		wantStatus LegStatus
		wantData   []string
		wantNote   string
	}{
		"success: data present is ok": {
			data:       []string{"a", "b"},
			wantStatus: StatusOK,
			wantData:   []string{"a", "b"},
		},
		"success: nil error and empty is empty": {
			data:       []string{},
			wantStatus: StatusEmpty,
			wantData:   []string{},
		},
		"success: nil error and nil slice is empty": {
			data:       nil,
			wantStatus: StatusEmpty,
		},
		"error: unsupported capability maps to unsupported": {
			err:        unsupported,
			wantStatus: StatusUnsupported,
			wantNote:   unsupported.Error(),
		},
		"error: other error maps to error": {
			err:        failure,
			wantStatus: StatusError,
			wantNote:   failure.Error(),
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			leg := LegFrom(tt.data, tt.err)
			if leg.Status != tt.wantStatus {
				t.Errorf("Status = %v, want %v", leg.Status, tt.wantStatus)
			}
			if tt.wantNote != "" && leg.Note != tt.wantNote {
				t.Errorf("Note = %q, want %q", leg.Note, tt.wantNote)
			}
			if tt.wantStatus == StatusOK || tt.wantStatus == StatusEmpty {
				if len(leg.Data) != len(tt.wantData) {
					t.Errorf("Data = %v, want %v", leg.Data, tt.wantData)
				}
			}
		})
	}
}

// TestNoErrorTextMatchingInPackage guards the invariant that capability
// support is decided only via errors.Is on lsp.ErrUnsupported, never by
// matching error text. No non-test file in the package may substring-match an
// error message, so a future edit that reintroduces text matching fails here.
// The guard covers every stdlib substring primitive an edit might reach for,
// not only strings.Contains.
func TestNoErrorTextMatchingInPackage(t *testing.T) {
	t.Parallel()

	forbidden := []string{
		"strings.Contains",
		"strings.HasPrefix",
		"strings.HasSuffix",
		"strings.Index",
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, token := range forbidden {
			if strings.Contains(string(src), token) {
				t.Errorf("%s calls %s; capability support must be decided via errors.Is on lsp.ErrUnsupported, not error text", name, token)
			}
		}
	}
}
