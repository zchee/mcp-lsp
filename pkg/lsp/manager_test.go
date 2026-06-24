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
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// newTestManager returns a [Manager] whose sessions never spawn a subprocess: each
// session's startFn merely closes ready, incrementing spawns so tests can assert
// how many times a server would have been launched.
func newTestManager(spawns *atomic.Int64) *Manager {
	m := &Manager{
		cfg:      map[string]ServerConfig{"go": {LanguageID: protocol.LanguageKindGo}},
		sessions: make(map[string]*serverSession),
		logger:   slog.New(slog.DiscardHandler),
		rootURI:  uri.File("/workspace"),
	}
	m.newSessionFn = func(store *store, logger *slog.Logger) *serverSession {
		sess := newSession(store, logger)
		sess.startFn = func(context.Context, ServerConfig, uri.URI) {
			spawns.Add(1)
			close(sess.ready)
		}
		return sess
	}
	return m
}

func TestManagerSessionUnknownLanguage(t *testing.T) {
	t.Parallel()

	var spawns atomic.Int64
	m := newTestManager(&spawns)

	if _, err := m.session(t.Context(), "rust"); err == nil {
		t.Fatal("session returned nil error for an unknown language")
	}
	if got := spawns.Load(); got != 0 {
		t.Errorf("unknown language spawned %d servers, want 0", got)
	}
}

func TestNewManagerWorkspaceRootIsAbsolute(t *testing.T) {
	t.Parallel()

	m := NewManager(nil, ".", nil)
	if !filepath.IsAbs(m.WorkspaceRoot()) {
		t.Errorf("WorkspaceRoot = %q, want an absolute path", m.WorkspaceRoot())
	}
}

func TestManagerSessionSpawnsOnceConcurrently(t *testing.T) {
	t.Parallel()

	var spawns atomic.Int64
	m := newTestManager(&spawns)

	const callers = 16
	var wg sync.WaitGroup
	sessions := make([]*serverSession, callers)
	errs := make([]error, callers)
	wg.Add(callers)
	for i := range callers {
		go func() {
			defer wg.Done()
			sessions[i], errs[i] = m.session(t.Context(), "go")
		}()
	}
	wg.Wait()

	if got := spawns.Load(); got != 1 {
		t.Errorf("concurrent first calls spawned %d servers, want 1", got)
	}
	for i := range callers {
		if errs[i] != nil {
			t.Errorf("caller %d: unexpected error: %v", i, errs[i])
			continue
		}
		if sessions[i] != sessions[0] {
			t.Errorf("caller %d observed a different session than caller 0", i)
		}
	}
}

func TestManagerSessionReplacesDead(t *testing.T) {
	t.Parallel()

	var spawns atomic.Int64
	m := newTestManager(&spawns)

	first, err := m.session(t.Context(), "go")
	if err != nil {
		t.Fatalf("first session: %v", err)
	}

	// Mark the session dead, as the connection watchdog would on server death.
	first.dead.Store(true)

	second, err := m.session(t.Context(), "go")
	if err != nil {
		t.Fatalf("second session: %v", err)
	}

	if second == first {
		t.Error("dead session was reused instead of replaced")
	}
	if got := spawns.Load(); got != 2 {
		t.Errorf("expected 2 spawns after replacing a dead session, got %d", got)
	}
	// The replacement reuses the prior diagnostics cache so cached state survives.
	if second.store != first.store {
		t.Error("replacement session did not reuse the prior diagnostics cache")
	}
}

func TestManagerSessionContextCanceled(t *testing.T) {
	t.Parallel()

	m := &Manager{
		cfg:      map[string]ServerConfig{"go": {LanguageID: protocol.LanguageKindGo}},
		sessions: make(map[string]*serverSession),
		logger:   slog.New(slog.DiscardHandler),
		rootURI:  uri.File("/workspace"),
	}
	// startFn never closes ready, so session must observe the canceled context.
	m.newSessionFn = func(store *store, logger *slog.Logger) *serverSession {
		sess := newSession(store, logger)
		sess.startFn = func(context.Context, ServerConfig, uri.URI) {}
		return sess
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := m.session(ctx, "go"); err == nil {
		t.Fatal("session returned nil error for a canceled context")
	}
}
