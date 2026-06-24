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
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// fakeClock is a deterministic clock for driving the settle window without real
// sleeping.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Unix(0, 0)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
}

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

	clock := newFakeClock()
	s := newStoreWithClock(clock.Now)
	u := uri.File("/tmp/last_wins.go")
	const settle = 250 * time.Millisecond

	// An empty pre-analysis publish must not win over the real diagnostics that
	// arrive within the settle window.
	s.publish(publishParams(u, nil))
	s.publish(publishParams(u, errDiag()))

	parked := armParkSignal(s)

	resCh := make(chan waitSettledResult, 1)
	go func() {
		diags, err := s.waitSettled(t.Context(), u, settle)
		resCh <- waitSettledResult{diags: diags, err: err}
	}()

	// Once the waiter is parked, advance past the settle window and wake it.
	waitForPark(t, parked)
	clock.Advance(settle)
	s.broadcastAll()

	got := <-resCh
	if got.err != nil {
		t.Fatalf("waitSettled returned error: %v", got.err)
	}
	if diff := cmp.Diff(wantErrDiag(), flattenDiagnostics(got.diags)); diff != "" {
		t.Errorf("waitSettled diagnostics mismatch (-want +got):\n%s", diff)
	}
}

func TestStoreWaitSettledAfterIgnoresBaseline(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	s := newStoreWithClock(clock.Now)
	u := uri.File("/tmp/baseline.go")
	const settle = 250 * time.Millisecond

	// A settled publish at or before the caller's baseline is stale for a fresh
	// document open. waitSettledAfter must keep waiting until a later publish
	// arrives, then return that newer snapshot.
	s.publish(publishParams(u, nil))
	baselineSeq := s.publishSeq(u)
	clock.Advance(settle)

	parked := armParkSignal(s)

	resCh := make(chan waitSettledResult, 1)
	go func() {
		diags, err := s.waitSettledAfter(t.Context(), u, settle, baselineSeq)
		resCh <- waitSettledResult{diags: diags, err: err}
	}()

	waitForPark(t, parked)
	s.broadcastAll()
	waitForPark(t, parked)
	select {
	case got := <-resCh:
		t.Fatalf("waitSettledAfter returned stale baseline publish: diags=%v err=%v", got.diags, got.err)
	default:
	}

	s.publish(publishParams(u, errDiag()))
	waitForPark(t, parked)
	clock.Advance(settle)
	s.broadcastAll()

	got := <-resCh
	if got.err != nil {
		t.Fatalf("waitSettledAfter returned error: %v", got.err)
	}
	if diff := cmp.Diff(wantErrDiag(), flattenDiagnostics(got.diags)); diff != "" {
		t.Errorf("waitSettledAfter diagnostics mismatch (-want +got):\n%s", diff)
	}
}

func TestStoreWaitSettledDeadline(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	s := newStoreWithClock(clock.Now)
	u := uri.File("/tmp/deadline.go")

	// A publish was seen, but the clock never advances, so the window never
	// settles and the context deadline must end the wait.
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
}

func TestStoreWaitSettledNoPublishDeadline(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	s := newStoreWithClock(clock.Now)
	u := uri.File("/tmp/never.go")

	// No publish ever arrives; the wait ends only when the deadline passes.
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	if _, err := s.waitSettled(ctx, u, time.Hour); err == nil {
		t.Fatalf("waitSettled returned nil error, want context deadline")
	}
}

func TestStoreBroadcastAllReleases(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	s := newStoreWithClock(clock.Now)
	u := uri.File("/tmp/crash.go")
	const settle = time.Hour

	// A publish is seen but the settle window is effectively infinite; the crash
	// watchdog path (broadcastAll after the window elapses) must release the
	// waiter promptly with the latest snapshot.
	s.publish(publishParams(u, errDiag()))

	parked := armParkSignal(s)

	resCh := make(chan []protocol.Diagnostic, 1)
	errCh := make(chan error, 1)
	go func() {
		diags, err := s.waitSettled(t.Context(), u, settle)
		errCh <- err
		resCh <- diags
	}()

	waitForPark(t, parked)
	// Advance beyond the window and broadcast, simulating the watchdog firing.
	clock.Advance(settle)
	s.broadcastAll()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("waitSettled returned error: %v", err)
		}
		if diff := cmp.Diff(wantErrDiag(), flattenDiagnostics(<-resCh)); diff != "" {
			t.Errorf("waitSettled diagnostics mismatch (-want +got):\n%s", diff)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waitSettled did not return after broadcastAll")
	}
}

func TestStoreSnapshot(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	s := newStoreWithClock(clock.Now)
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

// armParkSignal installs the store's onWait test seam and returns a channel
// that receives once each time a waitSettled goroutine parks. The send is
// non-blocking so repeated parks never stall the waiter.
func armParkSignal(s *store) <-chan struct{} {
	parked := make(chan struct{}, 1)
	s.onWait = func() {
		select {
		case parked <- struct{}{}:
		default:
		}
	}

	return parked
}

// waitForPark blocks until a waitSettled goroutine parks or the budget expires.
func waitForPark(t *testing.T, parked <-chan struct{}) {
	t.Helper()

	select {
	case <-parked:
	case <-time.After(2 * time.Second):
		t.Fatal("no goroutine parked in waitSettled within the budget")
	}
}
