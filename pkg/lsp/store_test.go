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
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/go-cmp/cmp"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// diagMessage is the message carried by the single diagnostic the store tests
// publish and assert on.
const diagMessage = "boom"

// errDiag returns a single-element error diagnostic slice for assertions.
func errDiag() []protocol.Diagnostic {
	return []protocol.Diagnostic{
		{
			Severity: protocol.DiagnosticSeverityError,
			Message:  protocol.String(diagMessage),
		},
	}
}

// wantErrDiag is the flattened, comparable projection of errDiag.
// [protocol.Diagnostic] values carry unexported union/optional fields that
// go-cmp cannot inspect, so assertions compare the domain projection produced by
// flattenDiagnostics.
func wantErrDiag() []Diagnostic {
	return flattenDiagnostics(errDiag())
}

func publishParams(u uri.URI, diags []protocol.Diagnostic) *protocol.PublishDiagnosticsParams {
	return &protocol.PublishDiagnosticsParams{
		URI:         u,
		Diagnostics: diags,
	}
}

type waitSettledResult struct {
	diags []protocol.Diagnostic
	err   error
}

func TestStoreWaitSettledLastWins(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := newStore()
		u := uri.File("/tmp/last_wins.go")
		const settle = 250 * time.Millisecond

		// An empty pre-analysis publish must not win over the real diagnostics
		// that arrive within the settle window.
		s.publish(publishParams(u, nil))
		s.publish(publishParams(u, errDiag()))

		resCh := make(chan waitSettledResult, 1)
		go func() {
			diags, err := s.waitSettled(t.Context(), u, settle)
			resCh <- waitSettledResult{diags: diags, err: err}
		}()

		// Once the waiter is parked, fake time advances past the settle window
		// and the store's timer callback wakes it.
		synctest.Sleep(settle)

		got := <-resCh
		if got.err != nil {
			t.Fatalf("waitSettled returned error: %v", got.err)
		}
		if diff := cmp.Diff(wantErrDiag(), flattenDiagnostics(got.diags)); diff != "" {
			t.Errorf("waitSettled diagnostics mismatch (-want +got):\n%s", diff)
		}
	})
}

func TestStoreWaitSettledAfterIgnoresBaseline(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := newStore()
		u := uri.File("/tmp/baseline.go")
		const settle = 250 * time.Millisecond

		// A settled publish at or before the caller's baseline is stale for a
		// fresh document open. waitSettledAfter must keep waiting until a later
		// publish arrives, then return that newer snapshot.
		s.publish(publishParams(u, nil))
		baselineSeq := s.publishSeq(u)
		synctest.Sleep(settle)

		resCh := make(chan waitSettledResult, 1)
		go func() {
			diags, err := s.waitSettledAfter(t.Context(), u, settle, baselineSeq)
			resCh <- waitSettledResult{diags: diags, err: err}
		}()

		synctest.Wait()
		select {
		case got := <-resCh:
			t.Fatalf("waitSettledAfter returned stale baseline publish: diags=%v err=%v", got.diags, got.err)
		default:
		}

		s.publish(publishParams(u, errDiag()))
		synctest.Sleep(settle)

		got := <-resCh
		if got.err != nil {
			t.Fatalf("waitSettledAfter returned error: %v", got.err)
		}
		if diff := cmp.Diff(wantErrDiag(), flattenDiagnostics(got.diags)); diff != "" {
			t.Errorf("waitSettledAfter diagnostics mismatch (-want +got):\n%s", diff)
		}
	})
}

func TestStoreWaitSettledDeadline(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := newStore()
		u := uri.File("/tmp/deadline.go")

		// A publish was seen, but the settle window is much longer than the
		// deadline, so the context deadline must end the wait.
		s.publish(publishParams(u, errDiag()))

		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()

		diags, err := s.waitSettled(ctx, u, time.Hour)
		if err == nil {
			t.Fatalf("waitSettled returned nil error, want context deadline; diags=%v", diags)
		}
		if ctx.Err() == nil {
			t.Errorf("context was not done; err = %v", err)
		}
	})
}

func TestStoreWaitSettledNoPublishDeadline(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := newStore()
		u := uri.File("/tmp/never.go")

		// No publish ever arrives; the wait ends only when the deadline passes.
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()

		if _, err := s.waitSettled(ctx, u, time.Hour); err == nil {
			t.Fatalf("waitSettled returned nil error, want context deadline")
		}
	})
}

func TestStoreBroadcastAllReleases(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := newStore()
		u := uri.File("/tmp/crash.go")
		const settle = time.Hour

		// A publish is seen but the settle window is effectively infinite; the
		// watchdog path (broadcastAll after the window elapses) must release the
		// waiter with the latest snapshot.
		s.publish(publishParams(u, errDiag()))

		resCh := make(chan waitSettledResult, 1)
		go func() {
			diags, err := s.waitSettled(t.Context(), u, settle)
			resCh <- waitSettledResult{diags: diags, err: err}
		}()

		// Advance beyond the window, letting the store's timer callback
		// broadcast to the parked waiter.
		synctest.Sleep(settle)

		got := <-resCh
		if got.err != nil {
			t.Fatalf("waitSettled returned error: %v", got.err)
		}
		if diff := cmp.Diff(wantErrDiag(), flattenDiagnostics(got.diags)); diff != "" {
			t.Errorf("waitSettled diagnostics mismatch (-want +got):\n%s", diff)
		}
	})
}

func TestStoreSnapshot(t *testing.T) {
	t.Parallel()

	s := newStore()
	u := uri.File("/tmp/snapshot.go")

	if _, ok := s.snapshot(u); ok {
		t.Fatal("snapshot reported a document before any publish")
	}

	s.publish(publishParams(u, errDiag()))
	got, ok := s.snapshot(u)
	if !ok {
		t.Fatal("snapshot did not report a document after publish")
	}
	if diff := cmp.Diff(wantErrDiag(), flattenDiagnostics(got)); diff != "" {
		t.Errorf("snapshot mismatch (-want +got):\n%s", diff)
	}

	// The returned slice must be a copy: mutating it must not affect the cache.
	got[0].Message = protocol.String("mutated")
	again, _ := s.snapshot(u)
	if diff := cmp.Diff(wantErrDiag(), flattenDiagnostics(again)); diff != "" {
		t.Errorf("snapshot returned an aliased slice (-want +got):\n%s", diff)
	}
}
