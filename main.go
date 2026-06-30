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

// Command mcp-lsp runs language servers and exposes their capabilities to an AI
// agent as Model Context Protocol tools over stdio.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	phuslog "github.com/phuslu/log"
	"go.lsp.dev/protocol"

	"github.com/zchee/mcp-lsp/internal/version"
	"github.com/zchee/mcp-lsp/pkg/lsp"
	mcpserver "github.com/zchee/mcp-lsp/pkg/mcp"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "mcp-lsp:", err)
		os.Exit(1)
	}
}

// run parses flags and serves the MCP server until the process is signaled.
func run(args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	cfg, err := parseCLI(args, cwd)
	if err != nil {
		return err
	}

	if cfg.showVersion {
		fmt.Fprintln(os.Stdout, version.Version)
		return nil
	}

	// Logging goes to stderr only: stdout is the MCP stdio transport, and any
	// bytes written there would corrupt the protocol stream.
	logger := slog.New(phuslog.SlogNewJSONHandler(os.Stderr, &slog.HandlerOptions{
		AddSource: true,
		Level:     parseLevel(cfg.logLevel),
	}))

	lspCfg := lsp.DefaultConfig()
	if cfg.lspCommand != "" {
		lspCfg[cfg.lang] = lsp.ServerConfig{
			Command:    cfg.lspCommand,
			Args:       cfg.lspArgs,
			LanguageID: protocol.LanguageKind(cfg.lang),
		}
	}
	mgr := lsp.NewManager(lspCfg, cfg.workspace, logger)
	defer func() {
		if err := mgr.Close(context.WithoutCancel(context.Background())); err != nil {
			logger.Warn("language server shutdown reported errors", slog.Any("error", err))
		}
	}()

	srv := mcpserver.NewServer(mgr, logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("starting mcp-lsp", slog.String("workspace", cfg.workspace), slog.String("version", version.Version))
	if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil {
		return fmt.Errorf("run mcp server: %w", err)
	}
	return nil
}

type cliConfig struct {
	workspace   string
	logLevel    string
	showVersion bool
	lspCommand  string
	lspArgs     []string
	lang        string
}

type stringFlag struct {
	value string
	set   bool
}

func (f *stringFlag) Set(value string) error {
	f.value = value
	f.set = true
	return nil
}

func (f *stringFlag) String() string {
	return f.value
}

func parseCLI(args []string, cwd string) (cliConfig, error) {
	flagArgs, lspArgs, hasDelimiter := splitLSPArgs(args)

	fs := flag.NewFlagSet("mcp-lsp", flag.ContinueOnError)
	workspace := fs.String("workspace", cwd, "workspace root directory for the language servers")
	logLevel := fs.String("log-level", "info", "log level: debug, info, warn, or error")
	showVersion := fs.Bool("version", false, "print the version and exit")
	var lspCommand stringFlag
	fs.Var(&lspCommand, "lsp", "language server command")

	if err := fs.Parse(flagArgs); err != nil {
		return cliConfig{}, err
	}
	cfg := cliConfig{
		workspace:   *workspace,
		logLevel:    *logLevel,
		showVersion: *showVersion,
		lspCommand:  lspCommand.value,
	}
	if cfg.showVersion {
		return cfg, nil
	}
	if extra := fs.Args(); len(extra) > 0 {
		return cliConfig{}, fmt.Errorf("unexpected positional arguments before --: %s", strings.Join(extra, " "))
	}
	if lspCommand.set && lspCommand.value == "" {
		return cliConfig{}, fmt.Errorf("lsp command is required")
	}
	if len(lspArgs) > 0 && !lspCommand.set {
		return cliConfig{}, fmt.Errorf("language-server args after -- require -lsp")
	}

	if hasDelimiter && len(lspArgs) > 0 {
		cfg.lspArgs = slices.Clone(lspArgs)
	}
	return cfg, nil
}

func splitLSPArgs(args []string) (flagArgs []string, lspArgs []string, hasDelimiter bool) {
	for i, arg := range args {
		if arg == "--" {
			return args[:i], args[i+1:], true
		}
	}
	return args, nil, false
}

// parseLevel maps a log level name to its [slog.Level], defaulting to info.
func parseLevel(name string) slog.Level {
	switch name {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
