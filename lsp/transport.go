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
	"io"
	"os/exec"
	"sync"
)

// processTransport runs a language server as a child process and bridges its
// stdio to a jsonrpc2 stream. Read pulls from the server's stdout, Write pushes
// to its stdin, and Close tears the process down without leaking pipes.
type processTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	// stderrDone is closed once the stderr drain goroutine has returned, so Close
	// can join it and avoid leaking the goroutine past process exit.
	stderrDone chan struct{}

	closeOnce sync.Once
	closeErr  error
}

var _ io.ReadWriteCloser = (*processTransport)(nil)

// ServerOption configures a spawned language server.
type ServerOption func(*exec.Cmd)

// WithDir sets the working directory of the spawned language server, which most
// servers treat as the default workspace root.
func WithDir(dir string) ServerOption {
	return func(cmd *exec.Cmd) { cmd.Dir = dir }
}

// WithEnv sets the environment of the spawned language server. When unset the
// child inherits the parent process environment.
func WithEnv(env []string) ServerOption {
	return func(cmd *exec.Cmd) { cmd.Env = env }
}

// newProcessTransport spawns name with args as a language server and returns a
// transport bridged to its stdio. stderr is drained to a sink so a chatty
// server can never deadlock on a full stderr pipe; the sink defaults to
// [io.Discard] when nil.
//
// On any wiring failure the partially started process is cleaned up before the
// error is returned, so a failed call leaks neither pipes nor a child process.
func newProcessTransport(ctx context.Context, stderr io.Writer, name string, args []string, opts ...ServerOption) (*processTransport, error) {
	if stderr == nil {
		stderr = io.Discard
	}

	cmd := exec.CommandContext(ctx, name, args...)
	for _, opt := range opts {
		opt(cmd)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("lsp: wiring server stdin: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("lsp: wiring server stdout: %w", err)
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("lsp: wiring server stderr: %w", err)
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderrPipe.Close()
		return nil, fmt.Errorf("lsp: starting server %q: %w", name, err)
	}

	t := &processTransport{
		cmd:        cmd,
		stdin:      stdin,
		stdout:     stdout,
		stderrDone: make(chan struct{}),
	}

	// Drain stderr in the background. A language server that logs heavily would
	// otherwise block on a full stderr pipe once the OS buffer fills, since
	// nothing else reads it.
	go func() {
		defer close(t.stderrDone)
		_, _ = io.Copy(stderr, stderrPipe)
	}()

	return t, nil
}

// Read implements [io.ReadWriteCloser].
//
// Reading framed bytes from the server's stdout.
func (t *processTransport) Read(p []byte) (int, error) { return t.stdout.Read(p) }

// Write implements [io.ReadWriteCloser].
//
// Writing framed bytes to the server's stdin.
func (t *processTransport) Write(p []byte) (int, error) { return t.stdin.Write(p) }

// Close implements [io.ReadWriteCloser].
//
// Close shuts the server down: it closes stdin to signal end-of-input (the
// graceful exit trigger for a server already told to shut down), waits for the
// stderr drain to finish, and waits for the process to exit. It is safe to call
// more than once and reports the first teardown error.
//
// A non-zero exit status is not by itself an error here: a server that exits in
// response to stdin closing commonly reports a signal- or status-bearing exit,
// which the caller surfaces through the connection lifecycle instead.
func (t *processTransport) Close() error {
	t.closeOnce.Do(func() {
		closeErr := t.stdin.Close()

		// Closing stdin lets the server observe EOF and exit; draining stderr to
		// completion then guarantees the copy goroutine has returned before Wait
		// reaps the process and invalidates the pipe.
		<-t.stderrDone

		if waitErr := t.cmd.Wait(); waitErr != nil {
			if _, ok := errors.AsType[*exec.ExitError](waitErr); !ok {
				// A genuine wait failure (not merely a non-zero exit) is worth
				// surfacing; a non-zero exit on teardown is expected and ignored.
				closeErr = errors.Join(closeErr, waitErr)
			}
		}

		// stdout is closed by Wait once the process is reaped; closing it again is
		// harmless but unnecessary, so the explicit close is omitted to avoid a
		// spurious "file already closed" error.
		t.closeErr = closeErr
	})

	return t.closeErr
}

// pipeTransport bridges an in-memory [io.ReadWriteCloser] (such as one end of a
// [net.Pipe]) into a transport. It owns the underlying connection and closes it
// exactly once. It backs both the in-process server wiring used by tests and
// any caller that already holds a connected duplex stream.
type pipeTransport struct {
	rwc       io.ReadWriteCloser
	closeOnce sync.Once
	closeErr  error
}

var _ io.ReadWriteCloser = (*pipeTransport)(nil)

// newPipeTransport adapts an existing duplex stream into a transport.
func newPipeTransport(rwc io.ReadWriteCloser) *pipeTransport {
	return &pipeTransport{rwc: rwc}
}

// Read implements [io.ReadWriteCloser].
func (t *pipeTransport) Read(p []byte) (int, error) { return t.rwc.Read(p) }

// Write implements [io.ReadWriteCloser].
func (t *pipeTransport) Write(p []byte) (int, error) { return t.rwc.Write(p) }

// Close implements [io.ReadWriteCloser].
//
// Close closes the underlying stream exactly once and reports its close error.
func (t *pipeTransport) Close() error {
	t.closeOnce.Do(func() { t.closeErr = t.rwc.Close() })
	return t.closeErr
}
