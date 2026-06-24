# Repository Instructions

This file applies to the entire `github.com/zchee/mcp-lsp` repository. Higher-level
Codex/OMX instructions remain in force; use this file for repository-local
architecture, style, and verification rules.

## First step for every change

- Confirm the checkout before making claims or edits:
  - `pwd`
  - `git status --short`
  - `git branch --show-current`
  - `git rev-parse --short HEAD`
  - `go env GOMOD`
- Do not transfer assumptions between similarly named local checkouts such as
  `mcp-lsp`, `exp-lsp`, or other LSP/MCP experiments. Re-check the live path.
- Keep unrelated local changes unstaged and untouched. Include untracked files
  in status reports.

## Project shape

`mcp-lsp` is a Go 1.27 MCP server that runs downstream language servers and
exposes their capabilities to agents over MCP stdio.

```text
main.go                  CLI flags, slog setup, MCP stdio server wiring
internal/version/        build/version derivation
pkg/lsp/                 LSP subprocess, jsonrpc2 transport, protocol handling
pkg/mcp/                 MCP tool schemas, handlers, one-based presentation
tests/                   opt-in end-to-end tests against real language servers
hack/tools/              pinned local tool module for fmt/lint/test helpers
vendor/                  vendored third-party dependencies
.omc/, .omx/             planning/context artifacts; inspect when resuming work
```

### Boundary rules

- `pkg/lsp` owns language-server subprocess lifecycle, `go.lsp.dev/jsonrpc2`
  streams, `go.lsp.dev/protocol` requests/responses, protocol-to-domain
  conversion, caches, timers, and goroutine/process cleanup.
- `pkg/mcp` owns MCP typed input/output structs, JSON/schema tags, file I/O for
  tool inputs, default language selection, one-based coordinate conversion, and
  user/agent-facing tool behavior.
- `main.go` only wires flags, logging, version output, manager/server creation,
  signal handling, and stdio serving.
- Do not let `pkg/mcp` construct raw LSP requests or import `jsonrpc2`; add a
  small `pkg/lsp` feature API instead.
- Do not duplicate `go.lsp.dev/protocol` structs. Reuse protocol types and
  convert only at package boundaries when unions or schema reflection require it.

## MCP stdio and logging safety

- stdout is the MCP stdio transport. Never write logs, debug output, or progress
  messages to stdout in server mode; use stderr-backed logging.
- Treat language-server commands as trusted internal registry entries, not tool
  input. Keep this property if adding languages or configuration.
- All subprocesses, JSON-RPC connections, timers, goroutines, and file handles
  must be cleaned up deterministically on initialization failure, shutdown,
  context cancellation, and server death.
- A `jsonrpc2` connection may need explicit `Close` to unblock a reader parked in
  a frame. Do not rely on context cancellation alone for teardown.

## LSP/MCP behavior conventions

- Agent-facing coordinates are one-based. LSP wire coordinates stay zero-based.
  Convert only at the MCP boundary.
- Diagnostics currently support pull when advertised and push/publish fallback.
  Keep `PublishDiagnostics` non-blocking because it runs on the connection read
  path.
- For new LSP features, follow the sibling-feature pattern:
  - `pkg/lsp/<feature>.go` for protocol calls and flattening.
  - `pkg/mcp/<feature>.go` for tool schemas, validation, file reads, and output.
  - Register read-only tools in `pkg/mcp/server.go` when they do not mutate files.
- Existing `.omx/plans` and `.omx/specs` may contain accepted design context for
  future LSP tools such as `textDocument/definition`. Read them before resuming
  related work, then verify against live code.

## Go style

- Target Go 1.27. Prefer modern standard-library idioms available to this module.
- Use `any` instead of `interface{}`.
- Prefer `slices.Clone` for defensive slice copies.
- Use early returns and explicit error checks. Error strings should be lowercase
  and should not end with punctuation.
- Keep concerns separated: validation in MCP handlers, protocol/session logic in
  LSP code, and process wiring in `main.go`.
- Keep exported symbols documented with period-terminated Go doc comments.
- In Go doc comments, use linkable bracket syntax for exported identifiers when
  the target resolves in godoc/pkgsite output. Example: use
  `[protocol.LanguageKindGo]`, `[sync.Once]`, `[exec.Cmd.Wait]`, or
  `[Diagnostics.Lookup]` instead of bare or backticked exported references.
  Avoid bracket-linking prose, acronyms, protocol method strings, URLs, or local
  variable/member spellings that do not resolve.
- Prefer `t.Context()` in tests. Use `go-cmp` for structured assertions; do not
  add testify-style assertion dependencies.

## Dependencies and vendoring

- Dependencies are vendored. Do not hand-edit `vendor/` for normal source
  changes.
- If dependency versions change, update `go.mod`, `go.sum`, and `vendor/`
  coherently, and explain why the dependency change is necessary.
- Start with the standard library for small logic. Add dependencies only when
  they materially improve correctness, interoperability, or maintainability.

## Local commands

Fast checks for most code changes:

```sh
gofmt -w <changed-go-files>
go test ./...
go vet ./...
git diff --check
```

Repository tool-backed checks:

```sh
make fmt
make test
make lint
```

Notes:

- `make test` uses `gotestsum`, race mode, repository build tags, and excludes
  the opt-in `tests` package by construction.
- `make test/integration` and `make coverage` set `MCP_LSP_INTEGRATION=1` and
  require real language servers such as `gopls` on `PATH`.
- `make lint` installs/uses the pinned `hack/tools` `golangci-lint` and expects
  the worktree diff to stay stable after formatting in CI.
- For documentation-only changes, `git diff --check` plus a content review is
  usually sufficient; run Go tests when comments touch exported APIs or examples.

## CI expectations

The GitHub Actions workflow uses Go `1.27.0-rc.1`, Node 24, stable Rust,
`gopls`, `pyright`, `make coverage`, and `make lint` on Linux runners. If local
validation differs from CI, call out the gap explicitly.

## Commit hygiene

- Keep commits focused by concern: LSP implementation, MCP tool surface, tests,
  docs, and dependency/vendor changes should be separable when practical.
- Review `git diff --cached --stat` and `git diff --cached` before committing.
- Preserve the repository/global commit trailer convention:

```text
Co-authored-by: Codex <noreply@openai.com>
```
