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
	"log/slog"
	"path/filepath"
	"sync"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// Manager owns one language server per language. Servers are spawned lazily on
// first use and reused across calls; a server that dies is replaced on the next
// request for its language.
type Manager struct {
	mu       sync.Mutex
	cfg      map[string]ServerConfig
	sessions map[string]*serverSession
	logger   *slog.Logger
	rootDir  string
	rootURI  uri.URI

	// newSessionFn constructs an unstarted session. It defaults to newSession
	// and is a seam for tests that exercise the lifecycle without a subprocess.
	newSessionFn func(store *store, logger *slog.Logger) *serverSession
}

// NewManager returns a [Manager] that spawns the servers described by cfg, rooted
// at the workspace directory rootDir, logging through logger. A nil logger is
// replaced with a discarding logger.
func NewManager(cfg map[string]ServerConfig, rootDir string, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	if absRoot, err := filepath.Abs(rootDir); err == nil {
		rootDir = absRoot
	}
	return &Manager{
		cfg:          cfg,
		sessions:     make(map[string]*serverSession),
		logger:       logger,
		rootDir:      rootDir,
		rootURI:      uri.File(rootDir),
		newSessionFn: newSession,
	}
}

// WorkspaceRoot returns the absolute workspace root directory configured on m.
func (m *Manager) WorkspaceRoot() string {
	return m.rootDir
}

// session returns the initialized server session for lang, spawning it on first
// use. Concurrent first calls for the same language observe a single spawn via
// the session's [sync.Once] and block on the same ready signal. A session marked
// dead is discarded and replaced. It returns [context.Context.Err] if ctx is
// canceled while the session initializes.
func (m *Manager) session(ctx context.Context, lang string) (*serverSession, error) {
	cfg, ok := m.cfg[lang]
	if !ok {
		return nil, fmt.Errorf("unknown language %q", lang)
	}

	m.mu.Lock()
	sess := m.sessions[lang]
	if sess == nil || sess.dead.Load() {
		sess = m.newSessionFn(m.store(sess), m.logger)
		m.sessions[lang] = sess
	}
	m.mu.Unlock()

	sess.once.Do(func() {
		sess.startFn(ctx, cfg, m.rootURI)
	})

	select {
	case <-sess.ready:
		return sess, sess.initErr
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// store reuses the diagnostics cache from a prior session when replacing a dead
// one so cached diagnostics survive a respawn, otherwise it creates a fresh
// cache.
func (m *Manager) store(prev *serverSession) *store {
	if prev != nil && prev.store != nil {
		return prev.store
	}
	return newStore()
}

// Close shuts down every live session and joins their shutdown errors.
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	sessions := make([]*serverSession, 0, len(m.sessions))
	for _, sess := range m.sessions {
		sessions = append(sessions, sess)
	}
	clear(m.sessions)
	m.mu.Unlock()

	var errs []error
	for _, sess := range sessions {
		if err := sess.shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (m *Manager) sessionForFile(ctx context.Context, lang, absPath string) (*serverSession, protocol.LanguageKind, uri.URI, error) {
	sess, err := m.session(ctx, lang)
	if err != nil {
		return nil, "", "", err
	}
	cfg := m.cfg[lang]
	return sess, cfg.LanguageID, uri.File(absPath), nil
}
