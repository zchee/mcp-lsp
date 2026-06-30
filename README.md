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
Active downstream language servers come from, in order:

1. a runtime JSON config file,
2. PATH discovery for known language-server commands, unless `-discover=false`,
3. an explicit one-off `-lsp` CLI override.

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
workspace root. A minimal config looks like this:

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

<!-- badge links -->
[test]: https://github.com/zchee/mcp-lsp/actions/workflows/test.yaml
[pkg.go.dev]: https://pkg.go.dev/zchee/mcp-lsp
[module]: https://github.com/zchee/mcp-lsp/releases/latest
[codecov]: https://app.codecov.io/gh/zchee/mcp-lsp

[test-badge]: https://img.shields.io/github/actions/workflow/status/zchee/mcp-lsp/test.yaml?branch=main&style=for-the-badge&label=TEST&logo=github
[pkg.go.dev-badge]: https://img.shields.io/badge/pkg.go.dev-doc-00add8?style=for-the-badge&logo=go
[module-badge]: https://img.shields.io/github/release/zchee/mcp-lsp.svg?color=00add8&label=MODULE&style=for-the-badge&logo=go
[codecov-badge]: https://img.shields.io/codecov/c/github/zchee/mcp-lsp/main?logo=codecov&style=for-the-badge
