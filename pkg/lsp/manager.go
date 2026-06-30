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
	"sort"
	"sync"
	"time"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// Default timeout for operations that don't have a context deadline. This is a
// safety valve to avoid blocking indefinitely on a server that has gone away or
// otherwise failed to respond.
const defaultTimeout = 5 * time.Second

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
	indexed := indexConfig(cfg)
	return &Manager{
		cfg:          indexed,
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

// ConfiguredLanguages returns the canonical languages configured on m.
func (m *Manager) ConfiguredLanguages() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	languages := make([]string, 0, len(m.cfg))
	for lang := range m.cfg {
		languages = append(languages, lang)
	}
	sort.Strings(languages)
	return languages
}

// session returns the initialized server session for lang, spawning it on first
// use. Concurrent first calls for the same language observe a single spawn via
// the session's [sync.Once] and block on the same ready signal. A session marked
// dead is discarded and replaced. It returns [context.Context.Err] if ctx is
// canceled while the session initializes.
func (m *Manager) session(ctx context.Context, lang string) (*serverSession, error) {
	canonical, cfg, ok := m.configForLanguage(lang)
	if !ok {
		return nil, fmt.Errorf("unknown language %q", lang)
	}

	m.mu.Lock()
	sess := m.sessions[canonical]
	if sess == nil || sess.dead.Load() {
		sess = m.newSessionFn(m.store(sess), m.logger)
		m.sessions[canonical] = sess
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
	canonical, cfg, ok := m.configForLanguage(lang)
	if !ok {
		return nil, "", "", fmt.Errorf("unknown language %q", lang)
	}
	sess, err := m.session(ctx, canonical)
	if err != nil {
		return nil, "", "", err
	}
	return sess, cfg.LanguageID, uri.File(absPath), nil
}

func (m *Manager) configForLanguage(lang string) (string, ServerConfig, bool) {
	canonical := CanonicalLanguage(lang)
	cfg, ok := m.cfg[canonical]
	return canonical, cfg, ok
}

func indexConfig(cfg map[string]ServerConfig) map[string]ServerConfig {
	indexed := make(map[string]ServerConfig, len(cfg))
	for lang, serverCfg := range cfg {
		canonical := CanonicalLanguage(lang)
		serverCfg = cloneConfig(serverCfg)
		if serverCfg.LanguageID == "" {
			serverCfg.LanguageID = protocol.LanguageKind(canonical)
		}
		indexed[canonical] = serverCfg
	}
	return indexed
}
