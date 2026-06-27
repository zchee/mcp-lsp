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
	"maps"
	"path/filepath"
	"sync"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// Client implements the subset of [protocol.Client] that mcp-lsp needs. It
// embeds [protocol.UnimplementedClient] so un-overridden server->client requests
// return a well-formed "method not found" and un-overridden notifications are
// ignored, and overrides only the handlers that carry diagnostics or logs.
type Client struct {
	protocol.UnimplementedClient

	store  *store
	logger *slog.Logger

	applyMu     sync.Mutex
	applyPolicy workspaceEditApplyPolicy
}

var _ protocol.Client = (*Client)(nil)

type workspaceEditApplyPolicy struct {
	enabled bool
	options WorkspaceEditApplyOptions
}

// newClient returns a [Client] that records diagnostics into store and logs
// through logger.
func newClient(store *store, logger *slog.Logger) *Client {
	return &Client{
		store:  store,
		logger: logger,
	}
}

// PublishDiagnostics records the diagnostics for a document. It is dispatched
// off the connection read goroutine and must stay non-blocking: it only updates
// the cache and broadcasts.
func (c *Client) PublishDiagnostics(_ context.Context, params *protocol.PublishDiagnosticsParams) error {
	c.store.publish(params)
	return nil
}

// LogMessage forwards a window/logMessage notification to the structured logger.
func (c *Client) LogMessage(_ context.Context, params *protocol.LogMessageParams) error {
	c.logger.Debug("lsp log message", slog.String("message", params.Message))
	return nil
}

// ShowMessage forwards a window/showMessage notification to the structured logger.
func (c *Client) ShowMessage(_ context.Context, params *protocol.ShowMessageParams) error {
	c.logger.Info("lsp show message", slog.String("message", params.Message))
	return nil
}

// ApplyEdit handles server-initiated workspace/applyEdit requests under the
// currently enabled policy.
func (c *Client) ApplyEdit(_ context.Context, params *protocol.ApplyWorkspaceEditParams) (*protocol.ApplyWorkspaceEditResult, error) {
	if params == nil {
		reason := "applyEdit params are required"
		return &protocol.ApplyWorkspaceEditResult{Applied: false, FailureReason: &reason}, nil
	}

	decoded, err := WorkspaceEditFromProtocol(params.Edit)
	if err != nil {
		reason := fmt.Sprintf("convert applyEdit edit payload: %v", err)
		return &protocol.ApplyWorkspaceEditResult{Applied: false, FailureReason: &reason}, nil
	}

	policy, ok := c.currentApplyPolicy()
	if !ok {
		reason := "workspace/applyEdit is disabled by client policy"
		return &protocol.ApplyWorkspaceEditResult{
			Applied:       false,
			FailureReason: &reason,
		}, nil
	}

	result, err := ApplyWorkspaceEdit(decoded, policy)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) currentApplyPolicy() (options WorkspaceEditApplyOptions, ok bool) {
	c.applyMu.Lock()
	defer c.applyMu.Unlock()

	if !c.applyPolicy.enabled {
		return WorkspaceEditApplyOptions{}, false
	}

	options = c.applyPolicy.options
	options.CurrentVersions = cloneCurrentVersionsMap(options.CurrentVersions)
	return options, true
}

func (c *Client) withApplyEditPolicy(options WorkspaceEditApplyOptions, fn func() error) error {
	c.setApplyPolicy(true, options)
	defer c.setApplyPolicy(false, WorkspaceEditApplyOptions{})
	return fn()
}

func (c *Client) setApplyPolicy(enabled bool, options WorkspaceEditApplyOptions) {
	c.applyMu.Lock()
	defer c.applyMu.Unlock()

	if !enabled {
		c.applyPolicy = workspaceEditApplyPolicy{}
		return
	}

	c.applyPolicy = workspaceEditApplyPolicy{
		enabled: true,
		options: options,
	}
	c.applyPolicy.options.CurrentVersions = cloneCurrentVersionsMap(options.CurrentVersions)
}

func cloneCurrentVersionsMap(src map[string]uint32) map[string]uint32 {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]uint32, len(src))
	maps.Copy(dst, src)
	return dst
}

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
