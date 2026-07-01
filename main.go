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
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/go-json-experiment/json"
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

	registry, sources, err := loadRuntimeRegistry(&cfg)
	if err != nil {
		return err
	}
	mgr := lsp.NewManager(registry.ServerConfigs(), cfg.workspace, logger)
	defer func() {
		if err := mgr.Close(context.WithoutCancel(context.Background())); err != nil {
			logger.Warn("language server shutdown reported errors", slog.Any("error", err))
		}
	}()

	resolver := mcpserver.NewLanguageResolver(registry)
	srv := mcpserver.NewServer(mgr, logger, resolver)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info(
		"starting mcp-lsp",
		slog.String("workspace", cfg.workspace),
		slog.String("version", version.Version),
		slog.Any("languages", registry.ConfiguredLanguages()),
		slog.Any("sources", sources),
	)
	if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil {
		return fmt.Errorf("run mcp server: %w", err)
	}
	return nil
}

type cliConfig struct {
	workspace   string
	logLevel    string
	showVersion bool
	configPath  string
	discover    bool
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
	configPath := fs.String("config", "", "runtime language server registry JSON config")
	discover := fs.Bool("discover", true, "discover known language servers on PATH")
	var lspCommand stringFlag
	fs.Var(&lspCommand, "lsp", "language server command")
	var language stringFlag
	fs.Var(&language, "language", "language id served by -lsp; inferred for common servers when omitted")
	fs.Var(&language, "lang", "alias for -language")

	if err := fs.Parse(flagArgs); err != nil {
		return cliConfig{}, err
	}
	cfg := cliConfig{
		workspace:   *workspace,
		logLevel:    *logLevel,
		showVersion: *showVersion,
		configPath:  *configPath,
		discover:    *discover,
		lspCommand:  lspCommand.value,
		lang:        lsp.CanonicalLanguage(language.value),
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
	if language.set && cfg.lang == "" {
		return cliConfig{}, fmt.Errorf("language is required")
	}
	if len(lspArgs) > 0 && !lspCommand.set {
		return cliConfig{}, fmt.Errorf("language-server args after -- require -lsp")
	}
	if lspCommand.set && cfg.lang == "" {
		lang, ok := lsp.InferLanguageFromCommand(lspCommand.value)
		if !ok {
			return cliConfig{}, fmt.Errorf("language is required for -lsp %q; pass -language", lspCommand.value)
		}
		cfg.lang = lang
	}

	if hasDelimiter && len(lspArgs) > 0 {
		cfg.lspArgs = slices.Clone(lspArgs)
	}
	return cfg, nil
}

type runtimeConfigFile struct {
	Servers map[string]runtimeServerConfig `json:"servers"`
}

type runtimeServerConfig struct {
	Command    string   `json:"command"`
	Args       []string `json:"args"`
	LanguageID string   `json:"languageId"`
	Extensions []string `json:"extensions"`
	Aliases    []string `json:"aliases"`
	Shebangs   []string `json:"shebangs"`
}

func loadRuntimeRegistry(cfg *cliConfig) (*lsp.Registry, map[string]string, error) {
	specs := lsp.DefaultCatalog()
	servers := make(map[string]lsp.ServerConfig)
	sources := make(map[string]string)

	configPath, err := runtimeConfigPath(cfg)
	if err != nil {
		return nil, nil, err
	}
	if configPath != "" {
		configSpecs, configServers, err := loadRuntimeConfigFile(configPath)
		if err != nil {
			return nil, nil, err
		}
		specs = append(specs, configSpecs...)
		for lang, serverCfg := range configServers {
			servers[lang] = serverCfg
			sources[lang] = "config"
		}
	}

	if cfg.discover {
		discovered := lsp.DiscoverServerConfigs(specs, exec.LookPath)
		for lang, serverCfg := range discovered {
			if _, exists := servers[lang]; exists {
				continue
			}
			servers[lang] = serverCfg
			sources[lang] = "discovery"
		}
	}

	if cfg.lspCommand != "" {
		canonical, languageID, updatedSpecs, err := resolveCLIOverrideLanguage(specs, cfg.lang)
		if err != nil {
			return nil, nil, err
		}
		specs = updatedSpecs
		servers[canonical] = lsp.ServerConfig{
			Command:    cfg.lspCommand,
			Args:       slices.Clone(cfg.lspArgs),
			LanguageID: languageID,
		}
		sources[canonical] = "cli"
	}

	registry, err := lsp.NewRegistry(specs, servers)
	if err != nil {
		return nil, nil, err
	}
	return registry, sources, nil
}

func runtimeConfigPath(cfg *cliConfig) (string, error) {
	if cfg.configPath != "" {
		resolvedPath, err := filepath.Abs(cfg.configPath)
		if err != nil {
			return "", fmt.Errorf("resolve config path %q: %w", cfg.configPath, err)
		}
		return resolvedPath, nil
	}
	workspacePath := filepath.Join(cfg.workspace, ".mcp-lsp.json")
	// #nosec G304 G703 -- this checks the workspace-local runtime registry
	// filename documented for operators, not an MCP tool-provided path.
	if _, err := os.Stat(workspacePath); err == nil {
		return workspacePath, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat workspace config %q: %w", workspacePath, err)
	}
	globalPath, err := globalRuntimeConfigPath()
	if err != nil {
		return "", err
	}
	if globalPath != "" {
		return globalPath, nil
	}
	return "", nil
}

func globalRuntimeConfigPath() (string, error) {
	if path := strings.TrimSpace(os.Getenv("MCP_LSP_CONFIG")); path != "" {
		resolvedPath, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("resolve MCP_LSP_CONFIG %q: %w", path, err)
		}
		return resolvedPath, nil
	}

	configHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if configHome == "" {
		return "", nil
	}
	if !filepath.IsAbs(configHome) {
		return "", nil
	}

	path := filepath.Join(configHome, "mcp-lsp", "config.json")
	// #nosec G304 G703 -- this checks the documented user-level runtime
	// registry config path, not an MCP tool-provided path.
	if _, err := os.Stat(path); err == nil {
		return path, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat global config %q: %w", path, err)
	}
	return "", nil
}

func loadRuntimeConfigFile(path string) ([]lsp.LanguageSpec, map[string]lsp.ServerConfig, error) {
	// #nosec G304 G703 -- path is an explicit or discovered runtime registry
	// config path documented for operators, not untrusted tool input.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read config %q: %w", path, err)
	}
	var cfg runtimeConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, nil, fmt.Errorf("parse config %q: %w", path, err)
	}
	specs := make([]lsp.LanguageSpec, 0, len(cfg.Servers))
	servers := make(map[string]lsp.ServerConfig, len(cfg.Servers))
	seenLanguages := make(map[string]string, len(cfg.Servers))
	for language := range cfg.Servers {
		server := cfg.Servers[language]
		canonical := lsp.CanonicalLanguage(language)
		if canonical == "" {
			return nil, nil, fmt.Errorf("config language is required")
		}
		if previous, exists := seenLanguages[canonical]; exists {
			return nil, nil, fmt.Errorf("duplicate config language %q canonicalizes to %q already configured by %q", language, canonical, previous)
		}
		seenLanguages[canonical] = language
		languageID := protocol.LanguageKind(server.LanguageID)
		specs = append(specs, lsp.LanguageSpec{
			Language:   canonical,
			LanguageID: languageID,
			Aliases:    slices.Clone(server.Aliases),
			Extensions: slices.Clone(server.Extensions),
			Shebangs:   slices.Clone(server.Shebangs),
		})
		servers[canonical] = lsp.ServerConfig{
			Command:    server.Command,
			Args:       slices.Clone(server.Args),
			LanguageID: languageID,
		}
	}
	return specs, servers, nil
}

func resolveCLIOverrideLanguage(specs []lsp.LanguageSpec, lang string) (canonical string, languageID protocol.LanguageKind, updatedSpecs []lsp.LanguageSpec, err error) {
	registry, err := lsp.NewRegistry(specs, nil)
	if err != nil {
		return "", "", nil, err
	}
	if known, ok := registry.CanonicalLanguage(lang); ok {
		spec, _ := registry.LanguageSpec(known)
		return known, spec.LanguageID, specs, nil
	}
	canonical = strings.ToLower(strings.TrimSpace(lang))
	if canonical == "" {
		return "", "", nil, fmt.Errorf("language is required")
	}
	languageID = protocol.LanguageKind(canonical)
	updatedSpecs = append(slices.Clone(specs), lsp.LanguageSpec{Language: canonical, LanguageID: languageID})
	return canonical, languageID, updatedSpecs, nil
}

func splitLSPArgs(args []string) (flagArgs, lspArgs []string, hasDelimiter bool) {
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
