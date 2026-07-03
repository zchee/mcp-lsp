# mcp-lsp

[![test][test-badge]][test]
[![pkg.go.dev][pkg.go.dev-badge]][pkg.go.dev]
[![Go module][module-badge]][module]
[![codecov.io][codecov-badge]][codecov]

mcp-lsp provides an [MCP](https://modelcontextprotocol.io/introduction) server
that runs and exposes [Language Server Protocol](https://microsoft.github.io/language-server-protocol/)
capabilities to agents over MCP stdio.

## Runtime language registry

`mcp-lsp` does not activate hardcoded language-server defaults at startup.
Active downstream language servers come from these sources, in order of
precedence from highest to lowest:

1. an explicit one-off `-lsp` CLI override,
2. a runtime JSON config file,
3. PATH discovery for known language-server commands, unless `-discover=false`.

The built-in language catalog contains identity metadata such as aliases,
extensions, LSP language IDs, and discovery candidates. That catalog is not an
active server registry by itself.

```sh
# Infer Python from the common BasedPyright command.
mcp-lsp -lsp basedpyright-langserver -- --stdio

# Use a workspace-local or explicit runtime registry config.
mcp-lsp -config .mcp-lsp.json

# Disable PATH discovery for deterministic environments.
mcp-lsp -discover=false -config .mcp-lsp.json
```

If no config is supplied, `mcp-lsp` also checks for `.mcp-lsp.json` in the
workspace root. If the workspace does not provide `.mcp-lsp.json`, `mcp-lsp`
then checks a global runtime config. Set `MCP_LSP_CONFIG` to override the
global path; otherwise the default global path is
`$XDG_CONFIG_HOME/mcp-lsp/config.json` when `XDG_CONFIG_HOME` is set to an
absolute path. Missing workspace and default global configs are ignored; blank,
unset, or relative `XDG_CONFIG_HOME` values do not define a default global
config path. A missing or unreadable explicit `-config` path or
`MCP_LSP_CONFIG` path is an error.

Config file path precedence is:

1. `-config <path>`,
2. `<workspace>/.mcp-lsp.json`,
3. `MCP_LSP_CONFIG`,
4. `$XDG_CONFIG_HOME/mcp-lsp/config.json`.

A minimal config looks like this:

```json
{
  "servers": {
    "python": {
      "command": "basedpyright-langserver",
      "args": ["--stdio"],
      "languageId": "python",
      "extensions": [".py", ".pyi"],
      "aliases": ["py", "pyright", "basedpyright"]
    },
    "go": {
      "command": "gopls",
      "languageId": "go",
      "extensions": [".go"]
    }
  }
}
```

One MCP process can manage multiple languages. Each downstream LSP process is
spawned lazily on the first request for its configured language and then reused.

For file-based tools such as diagnostics, hover, definition, formatting, rename,
code actions, and code lenses, `language` may be omitted when the file extension
or shebang identifies a configured language. Explicit `language` still wins for
generated or unusual files. If a file extension is unknown, or the matching
language has no configured server, the tool returns a clear error rather than
routing to another language.

File-less tools are deterministic:

- `lsp_workspace_symbol` uses the explicit `language`, or the single configured
  language when exactly one server is active.
- `lsp_execute_command` never fans out across multiple language servers; omit
  `language` only when exactly one server is configured.

## Composite tools

Alongside the thin, one-request-per-tool wrappers, `mcp-lsp` exposes composite
tools that fuse several language-server requests into one call, returning
judgement an agent cannot cheaply assemble by chaining raw tools:

- `lsp_impact_analysis` — the blast radius of changing a symbol: references,
  call graph, type graph, implementations, and diagnostics. References is the
  epicenter; the rest fan out and degrade independently.
- `lsp_symbol_context` — a dense symbol card: hover, enclosing outline,
  signature, navigation targets, same-file occurrences, and inlay context.
  Hover is the epicenter.
- `lsp_change_guard` — a verify-after-edit report over the post-edit, on-disk
  state of a changed file, ending in an advisory verdict. Diagnostics is the
  epicenter.

All three are read-only and share a metadata block: `readiness`, `stopReason`,
`epicenterTextHash` (a SHA-256 of the file text the call actually analyzed, so a
concurrent on-disk edit is detectable), and `capabilitiesUsed`/
`capabilitiesMissing`.

### Degradation contract

Each composite has one **epicenter** leg that is fatal when its capability is
unsupported — `lsp_impact_analysis` needs references, `lsp_symbol_context` needs
hover, and `lsp_change_guard` needs diagnostics — and the tool returns an error
in that case. Every other leg degrades on its own, carrying a per-leg `status`:

- `ok` / `empty` — the leg ran; `empty` is a trustworthy zero, only reported
  after a readiness check.
- `unsupported` — the server does not advertise the capability. This is
  distinct from `empty`, so an absent capability is never read as "no results".
- `truncated` — a budget cap was hit and the data is partial.
- `error` — the leg failed for a reason other than an unsupported capability.
- `notReady` — the leg could not be trusted within its readiness budget (the
  server was likely still indexing, or a fan-out deadline cut it short).

Because capabilities differ per server, the same composite returns different
legs as `unsupported` on different servers: `lsp_impact_analysis` reports the
declaration leg `unsupported` on gopls and the type-graph leg `unsupported` on
rust-analyzer and basedpyright.

### Readiness and the advisory verdict

Reference, call-hierarchy, and type-hierarchy legs are readiness-gated: a
language server returns empty or partial results while it is still indexing, so
an empty result is only trusted once two consecutive lookups agree. An
epicenter that never stabilizes yields `readiness: notReady` and skips the rest
of the analysis rather than reporting a misleading zero.

`lsp_change_guard`'s `advisoryVerdict` (`clean` / `attention` / `broken`) is
advisory and settle-gated: it reflects only static diagnostics settled at
analysis time — not tests or runtime — and `clean` is emitted only when
diagnostics are both ready and empty. Cold or unsettled diagnostics yield
`notReady`, never a false `clean`. The `basis` field names exactly which legs
produced the verdict, and the agent owns the decision to ship.

### Provenance

Composite input is disk-only: the tools read the named file from disk at call
time, so an agent must write its edits to disk before calling. Unsaved editor
buffers are not visible.

<!-- badge links -->
[test]: https://github.com/zchee/mcp-lsp/actions/workflows/test.yaml
[pkg.go.dev]: https://pkg.go.dev/zchee/mcp-lsp
[module]: https://github.com/zchee/mcp-lsp/releases/latest
[codecov]: https://app.codecov.io/gh/zchee/mcp-lsp

[test-badge]: https://img.shields.io/github/actions/workflow/status/zchee/mcp-lsp/test.yaml?branch=main&style=for-the-badge&label=TEST&logo=github
[pkg.go.dev-badge]: https://img.shields.io/badge/pkg.go.dev-doc-00add8?style=for-the-badge&logo=go
[module-badge]: https://img.shields.io/github/release/zchee/mcp-lsp.svg?color=00add8&label=MODULE&style=for-the-badge&logo=go
[codecov-badge]: https://img.shields.io/codecov/c/github/zchee/mcp-lsp/main?logo=codecov&style=for-the-badge
