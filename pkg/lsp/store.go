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
	"slices"
	"sync"
	"time"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// docState is the cached diagnostics for a single document URI.
type docState struct {
	diags   []protocol.Diagnostic
	version int32
	lastPub time.Time
	seq     uint64
}

// store is the authoritative per-URI diagnostics cache. The language server
// pushes textDocument/publishDiagnostics unsolicited and repeatedly, so the
// cache is the single source of truth that both the pull and push read paths
// converge on.
//
// The publish path runs off the jsonrpc2 dispatch goroutine and must never
// block: publish only swaps the slice, stamps the time, bumps the sequence, and
// broadcasts. waitSettled blocks a reader until the publish stream goes quiet
// for the settle window, the context is done, or the connection dies.
type store struct {
	mu    sync.Mutex
	cond  *sync.Cond
	docs  map[uri.URI]*docState
	nowFn func() time.Time

	// onWait, when non-nil, is invoked under the lock immediately before a
	// waitSettled goroutine parks on the condition variable. It is a test seam
	// for deterministically observing a parked waiter and is nil in production.
	onWait func()
}

// newStore returns a store whose clock is time.Now. Tests inject a fake clock
// with newStoreWithClock to drive the settle window deterministically.
func newStore() *store {
	return newStoreWithClock(time.Now)
}

// newStoreWithClock returns a store that reads the current time from nowFn.
func newStoreWithClock(nowFn func() time.Time) *store {
	s := &store{
		docs:  make(map[uri.URI]*docState),
		nowFn: nowFn,
	}
	s.cond = sync.NewCond(&s.mu)

	return s
}

// publish records the diagnostics carried by a textDocument/publishDiagnostics
// notification. It runs on the dispatch goroutine and only mutates the cache
// and broadcasts: no I/O, no blocking.
func (s *store) publish(params *protocol.PublishDiagnosticsParams) {
	version, _ := params.Version.Get()

	s.mu.Lock()
	defer s.mu.Unlock()

	doc := s.docs[params.URI]
	if doc == nil {
		doc = new(docState)
		s.docs[params.URI] = doc
	}
	doc.diags = slices.Clone(params.Diagnostics)
	doc.version = version
	doc.lastPub = s.nowFn()
	doc.seq++

	s.cond.Broadcast()
}

// snapshot returns a copy of the currently cached diagnostics for u and whether
// any publish has been seen for it.
func (s *store) snapshot(u uri.URI) ([]protocol.Diagnostic, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	doc := s.docs[u]
	if doc == nil {
		return nil, false
	}

	return slices.Clone(doc.diags), true
}

// waitSettled blocks until the publish stream for u has been quiet for at least
// settle since the last publish, returning the last-published diagnostics. It
// returns ctx.Err() if ctx is canceled or its deadline passes first, and
// returns promptly with the latest snapshot if broadcastAll fires (server
// death). It never sleeps past the context deadline.
//
// Cond.Wait cannot select on ctx, so a context.AfterFunc and a per-iteration
// time.AfterFunc watchdog wake the Cond when the deadline or settle boundary is
// reached; both are stopped on return.
func (s *store) waitSettled(ctx context.Context, u uri.URI, settle time.Duration) ([]protocol.Diagnostic, error) {
	stopCtx := context.AfterFunc(ctx, s.broadcastAll)
	defer stopCtx()

	s.mu.Lock()
	defer s.mu.Unlock()

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		doc := s.docs[u]
		if doc != nil && doc.seq > 0 {
			elapsed := s.nowFn().Sub(doc.lastPub)
			if elapsed >= settle {
				return slices.Clone(doc.diags), nil
			}

			timer := time.AfterFunc(settle-elapsed, s.broadcastAll)
			s.wait()
			timer.Stop()

			continue
		}

		s.wait()
	}
}

// wait parks the caller on the condition variable, signaling a test seam first
// when configured. The caller must hold s.mu.
func (s *store) wait() {
	if s.onWait != nil {
		s.onWait()
	}
	s.cond.Wait()
}

// broadcastAll wakes every waiter. The crash watchdog calls it on connection
// death so readers stop waiting on diagnostics that will never arrive, and the
// settle/deadline watchdogs call it to re-evaluate their wait condition.
func (s *store) broadcastAll() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cond.Broadcast()
}
