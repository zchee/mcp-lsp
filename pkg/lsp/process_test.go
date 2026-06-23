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
	"net"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"testing"
	"time"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// fakeServerEnv selects the fake-language-server behavior when the test binary
// re-executes itself as a subprocess. It is empty during normal test runs.
const fakeServerEnv = "MCP_LSP_FAKE_SERVER"

// TestMain intercepts re-executions of the test binary that carry fakeServerEnv
// and runs the requested fake-server behavior instead of the test suite. This
// gives the partial-init lifecycle test a real OS subprocess with real stdio
// pipes, so the genuine exec.Cmd spawn and wait paths are exercised without
// depending on an installed language server.
func TestMain(m *testing.M) {
	if os.Getenv(fakeServerEnv) == "exit-immediately" {
		// Emulate a server whose process starts but whose stream closes before
		// the initialize handshake completes: exiting closes stdout, so the
		// client's Initialize call fails with EOF and start takes the failStart
		// path while the child still needs reaping.
		os.Exit(0)
	}

	os.Exit(m.Run())
}

// TestSessionStartReapsProcessOnInitFailure verifies the partial-init cleanup
// fix: when the subprocess starts but the initialize handshake fails, start ->
// failStart must reap the child (so cmd.Wait was called and ProcessState is
// populated) rather than leaving a zombie that a later, never-arriving shutdown
// was supposed to collect. A failed session is replaced wholesale by the
// Manager, dropping this pointer, so the cleanup must happen in failStart.
func TestSessionStartReapsProcessOnInitFailure(t *testing.T) {
	before := runtime.NumGoroutine()

	cfg := ServerConfig{Command: os.Args[0], LanguageID: protocol.LanguageKindGo}
	sess := newSession(newStore(), slog.New(slog.DiscardHandler))
	t.Setenv(fakeServerEnv, "exit-immediately")

	sess.startFn(t.Context(), cfg, uri.File(t.TempDir()))
	<-sess.ready

	if sess.initErr == nil {
		t.Fatal("start did not report an initialization error for a server that exits before the handshake")
	}
	if !sess.dead.Load() {
		t.Error("a failed session was not marked dead")
	}
	if sess.cmd == nil {
		t.Fatal("session recorded no command despite a successful spawn")
	}

	// The decisive check: failStart must have waited on the process. A reaped
	// process has a non-nil ProcessState; a leaked (un-waited) one does not.
	waitForProcessState(t, sess.cmd, 5*time.Second)

	// No watchdog goroutine is started for a failed session and the read loop
	// must have exited, so the goroutine count returns near baseline.
	waitForGoroutines(t, before, 5*time.Second)
}

// blockingShutdownServer is an in-memory protocol.Server whose Shutdown blocks
// until its context is canceled, emulating a wedged server that never answers
// the shutdown request. It completes the initialize handshake normally so the
// session reaches a ready state before shutdown is exercised.
type blockingShutdownServer struct {
	protocol.UnimplementedServer

	shutdownEntered chan struct{}
	once            sync.Once
}

func (s *blockingShutdownServer) Initialize(_ context.Context, _ *protocol.InitializeParams) (*protocol.InitializeResult, error) {
	return &protocol.InitializeResult{ServerInfo: protocol.ServerInfo{Name: "blocking"}}, nil
}

func (s *blockingShutdownServer) Shutdown(ctx context.Context) error {
	s.once.Do(func() { close(s.shutdownEntered) })
	<-ctx.Done() // never returns until the caller's bounded context expires

	return ctx.Err()
}

// TestSessionShutdownBoundedOnWedgedServer verifies the bounded-handshake fix:
// doShutdown called with a context that has no deadline (exactly what
// Manager.Close passes) must still return promptly when the server never
// answers shutdown, because the handshake is bounded by shutdownWait and
// conn.Close forces teardown. The server runs in-memory over a pipe so the
// blocking-Shutdown scenario is deterministic without a real subprocess.
func TestSessionShutdownBoundedOnWedgedServer(t *testing.T) {
	t.Parallel()

	fake := &blockingShutdownServer{shutdownEntered: make(chan struct{})}
	sess := wireSessionWithServer(t, fake, false)

	done := make(chan error, 1)
	go func() { done <- sess.shutdown(context.WithoutCancel(t.Context())) }()

	// shutdownWait bounds the handshake and again the process wait (there is no
	// real process here, so the second budget is a no-op); allow generous slack.
	// The assertion is that shutdown returns at all rather than blocking forever
	// on the unanswered Shutdown call.
	select {
	case <-done:
	case <-time.After(3 * shutdownWait):
		t.Fatalf("shutdown did not return within %s on a wedged server: the handshake is unbounded", 3*shutdownWait)
	}

	select {
	case <-fake.shutdownEntered:
	default:
		t.Error("server Shutdown was never invoked")
	}
}

// waitForProcessState polls until cmd.ProcessState is populated (the process was
// waited on) or the timeout elapses.
func waitForProcessState(t *testing.T, cmd *exec.Cmd, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cmd.ProcessState != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("process was never reaped (cmd.ProcessState stayed nil): a failed-init subprocess leaked")
}

// waitForGoroutines polls until the goroutine count returns to within a small
// margin of baseline or the timeout elapses, tolerating scheduler lag.
func waitForGoroutines(t *testing.T, baseline int, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= baseline+1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("goroutine count %d did not return near baseline %d; a goroutine may have leaked", runtime.NumGoroutine(), baseline)
}

// wireSessionWithServer connects an arbitrary protocol.Server to a ready
// serverSession over an in-memory pipe, mirroring wireSession but accepting a
// custom server implementation.
func wireSessionWithServer(t *testing.T, srv protocol.Server, pull bool) *serverSession {
	t.Helper()

	clientEnd, serverEnd := net.Pipe()

	serverCtx, serverCancel := context.WithCancel(context.Background())
	_, serverConn, _ := protocol.NewServer(serverCtx, srv, jsonrpc2.NewHeaderStream(serverEnd))

	st := newStore()
	logger := slog.New(slog.DiscardHandler)
	clientCtx, clientCancel := context.WithCancel(context.Background())
	lspClient := newClient(st, logger)
	_, clientConn, server := protocol.NewClient(clientCtx, lspClient, jsonrpc2.NewHeaderStream(clientEnd))

	if _, err := server.Initialize(clientCtx, initializeParams(uri.File(t.TempDir()))); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := server.Initialized(clientCtx, &protocol.InitializedParams{}); err != nil {
		t.Fatalf("initialized: %v", err)
	}

	sess := &serverSession{
		ready:         make(chan struct{}),
		pullSupported: pull,
		conn:          clientConn,
		server:        server,
		client:        lspClient,
		store:         st,
		logger:        logger,
		cancel:        clientCancel,
	}
	sess.once.Do(func() {})
	close(sess.ready)

	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
		clientCancel()
		serverCancel()
	})

	return sess
}
