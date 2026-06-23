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

// Command mcp-lsp drives a downstream language server and reports the
// diagnostics it produces for a single file, exercising both LSP diagnostic
// models:
//
//   - the push model, where the server volunteers diagnostics through the
//     textDocument/publishDiagnostics notification after the document is opened;
//   - the pull model, where the client requests diagnostics with the LSP 3.17
//     textDocument/diagnostic request.
//
// With -serve it instead runs as an MCP server over stdio, exposing the
// language server's diagnostics to agents through the lsp_diagnostics tool; each
// tool call names the file, language, and diagnostic model to use.
//
// Usage:
//
//	mcp-lsp -serve -- gopls serve
//	mcp-lsp -file ./main.go -lang go -- gopls serve
//	mcp-lsp -file ./main.go -lang go -pull -- gopls serve
//	mcp-lsp -file ./src/lib.rs -lang rust -- rust-analyzer
//
// Everything after "--" is the command (and arguments) used to spawn the
// language server over stdio.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"go.lsp.dev/uri"

	"github.com/zchee/mcp-lsp/lsp"
	"github.com/zchee/mcp-lsp/mcp"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "mcp-lsp: %v\n", err)
		os.Exit(1)
	}
}

// config holds the parsed command-line invocation: how to serve, the document
// to analyze in one-shot mode, the language server command to spawn, and how
// diagnostics should be collected.
type config struct {
	serve      bool
	targetFile string
	language   string
	pull       bool
	wait       time.Duration
	server     []string
	logfile    string
}

// run parses args, spawns the language server, and either serves the MCP
// diagnostic tool over stdio or runs a single one-shot diagnostic collection. It
// is separated from main so the exit path has a single error funnel and so the
// flow is testable without process-level side effects.
func run(args []string) error {
	cfg, err := parseArgs(args)
	if err != nil {
		return err
	}

	logger, err := initLogger(cfg.logfile)
	if err != nil {
		return err
	}

	// The workspace root anchors the language server. In serve mode every tool
	// call supplies its own file, so the root is the working directory; in
	// one-shot mode it is the directory of the target file.
	rootDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolving working directory: %w", err)
	}
	if !cfg.serve {
		abs, err := filepath.Abs(cfg.targetFile)
		if err != nil {
			return fmt.Errorf("resolving %q: %w", cfg.targetFile, err)
		}
		rootDir = filepath.Dir(abs)
	}
	rootURI := uri.File(rootDir)

	// Tie SIGINT/SIGTERM to context cancellation. exec.CommandContext (used by
	// the transport) kills the child when this context is done, so a Ctrl-C
	// propagates all the way to the spawned server.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client, err := lsp.NewProcessClient(ctx, os.Stderr, cfg.server[0], cfg.server[1:])
	if err != nil {
		return err
	}
	defer func() {
		// Best-effort clean teardown: close stdin so the server sees EOF and exits,
		// then reap it. A teardown error is reported but does not mask a run error.
		if cerr := client.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "mcp-lsp: closing client: %v\n", cerr)
		}
	}()

	srv, err := mcp.NewServer(ctx, client, rootURI, logger)
	if err != nil {
		return err
	}

	if cfg.serve {
		return srv.Serve(ctx)
	}

	mode := "push"
	if cfg.pull {
		mode = "pull"
	}
	return srv.RunOnce(ctx, os.Stdout, mcp.DiagnosticInput{
		File:        cfg.targetFile,
		Language:    cfg.language,
		Mode:        mode,
		WaitSeconds: int(cfg.wait.Seconds()),
	})
}

// parseArgs parses the command-line flags and the trailing language-server
// command. It enforces that a server command is supplied after "--" and that
// the document path is non-empty.
func parseArgs(args []string) (*config, error) {
	cfg := &config{}

	fs := flag.NewFlagSet("mcp-lsp", flag.ContinueOnError)
	fs.BoolVar(&cfg.serve, "serve", false, "serve the diagnostic tool as an MCP server over stdio instead of running once")
	fs.StringVar(&cfg.logfile, "logfile", "", "if set, enable MCP server logging")
	fs.StringVar(&cfg.targetFile, "file", "", "path to the document to analyze (required unless -serve)")
	fs.StringVar(&cfg.language, "lang", "go", "LSP language identifier for the document (e.g. go, rust, typescript)")
	fs.BoolVar(&cfg.pull, "pull", false, "use the pull model (textDocument/diagnostic) instead of waiting for pushed diagnostics")
	fs.DurationVar(&cfg.wait, "wait", 10*time.Second, "how long to wait for pushed diagnostics in push mode")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: mcp-lsp [flags] -- <language-server> [server-args...]\n\nFlags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(fs.Output(), "\nExamples:\n")
		fmt.Fprintf(fs.Output(), "  mcp-lsp -serve -- gopls serve\n")
		fmt.Fprintf(fs.Output(), "  mcp-lsp -file ./main.go -lang go -- gopls serve\n")
	}

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	cfg.server = fs.Args()
	if len(cfg.server) == 0 {
		return nil, errors.New("no language server command supplied; pass it after \"--\", e.g. -- gopls serve")
	}
	if !cfg.serve && cfg.targetFile == "" {
		return nil, errors.New("the -file flag is required unless -serve is set")
	}

	return cfg, nil
}

func initLogger(f string) (*slog.Logger, error) {
	var logWriter io.WriteCloser

	handler := slog.DiscardHandler
	if f != "" {
		file, err := os.OpenFile(f, os.O_RDWR|os.O_CREATE, 0o600)
		if err != nil {
			return nil, fmt.Errorf("open %q file: %w", f, err)
		}
		logWriter = file

		handler = slog.NewJSONHandler(logWriter, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)

	return logger, nil
}
