# LSP reference

Use this file as a compact checklist. For exact field definitions, read the
official specification or meta model for the target version.

## Official sources

- Official site: https://microsoft.github.io/language-server-protocol/
- Current checked spec on 2026-06-24: https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/
- Source markdown: https://github.com/microsoft/language-server-protocol/blob/gh-pages/_specifications/lsp/3.18/specification.md
- Meta model: https://github.com/microsoft/language-server-protocol/tree/gh-pages/_specifications/lsp/3.18/metaModel

As checked on 2026-06-24, the official 3.18 markdown describes the current
3.18.x protocol and lists 3.18.0 with date 06/04/2026. Re-check the official
site before asserting the latest version, implementing a protocol upgrade, or
using a newly added feature.

## 3.18 areas to notice

The 3.18 changelog includes these areas. Verify exact names and capability
fields in the spec before implementation.

- Inline completions.
- Dynamic text document content.
- Folding range refresh.
- Formatting multiple ranges in one request.
- Snippets in workspace edits and text document edits.
- Relative patterns in document and notebook filters.
- Code action kind documentation.
- Nullable `activeParameter` in signature help structures.
- Tooltips for commands.
- Workspace edit metadata.
- Debug message kind.
- Code lens resolve-property enumeration.
- `completionList.applyKind` behavior for item defaults.

## Base protocol checks

LSP uses JSON-RPC messages with LSP framing. For stdio transports, stdout is the
framed stream; logs must go to stderr or a separate channel.

```text
Header: Content-Length: <bytes>\r\n
        Content-Type: application/vscode-jsonrpc; charset=utf-8\r\n
        \r\n
Body:   JSON-RPC payload encoded as UTF-8
```

Checklist:

- Count bytes, not runes, in `Content-Length`.
- Accept header order variations and optional content type where the transport
  library supports it.
- Preserve request IDs across response paths.
- Treat notifications as no-response messages.
- Handle cancellation with `$/cancelRequest`; cancellation is best-effort.
- Do not let server log output corrupt framed streams.

## Lifecycle sequence

```text
client                     server
  | -- initialize ---------> |
  | <- InitializeResult ---- |
  | -- initialized --------> |
  | -- feature requests ---> |
  | <- server requests ----- |  e.g. workspace/applyEdit
  | -- shutdown -----------> |
  | <- null ---------------- |
  | -- exit ---------------> |
```

Checklist:

- Do not send normal LSP requests before `initialize` completes unless the spec
  explicitly allows them.
- Respect `InitializeParams` and `InitializeResult` capabilities.
- Support dynamic `client/registerCapability` and
  `client/unregisterCapability` when advertised.
- Treat `shutdown` and `exit` as separate lifecycle steps.
- Clean up subprocesses, pipes, readers, writers, timers, goroutines, watchers,
  and caches on any initialization or shutdown failure.

## Capability negotiation

Always gate optional behavior on advertised capabilities.

| Area | Common gate |
| --- | --- |
| Document sync | `textDocumentSync` kind/options |
| Definition/references/hover | server provider booleans/options |
| Completion | `completionProvider`, trigger characters, resolve support |
| Diagnostics | push publish support vs pull diagnostic provider |
| Formatting | document/range/on-type/ranges formatting providers |
| Semantic tokens | token legend, full/range/delta support |
| Code actions | code action provider and resolve support |
| Workspace edits | resource operations, change annotations, snippet support |
| File watching | static options or dynamic registration |
| Position encoding | negotiated encoding when supported |

If a capability is missing, return a clear unsupported result or degrade to a
safe fallback instead of sending a request the server does not advertise.

## Positions, ranges, and URIs

- LSP line and character positions are zero-based.
- User-facing CLIs and tools often use one-based coordinates; convert at the
  outermost boundary.
- Character offsets are not bytes. Respect negotiated position encoding. Older
  clients/servers commonly imply UTF-16 code units.
- Keep ranges half-open: start inclusive, end exclusive.
- Preserve document versions where the method includes them.
- Use `DocumentUri`/URI parsing for `file://` handling; do not concatenate paths.
- Normalize paths only after URI decoding and platform-aware conversion.

## Text document synchronization

- `didOpen` establishes text and version state.
- `didChange` uses full or incremental sync depending on negotiated sync kind.
- `willSave` and `willSaveWaitUntil` are optional and timing-sensitive.
- `didSave` may include text only when configured.
- `didClose` should clear document-specific state and may clear diagnostics.

For diagnostics, consider both push and pull modes. Keep publish handlers
non-blocking when they execute on the JSON-RPC read path.

## Request result shapes

Many LSP methods return unions, nullable values, arrays, or partial-result
streams. Check exact result types before flattening.

Examples:

- Definition-like methods may return a single `Location`, many locations, or
  location links.
- Hover may contain markup content or marked strings depending on version and
  client capability.
- Completion may return an array or `CompletionList`; items may be resolved
  later.
- Code actions may return commands, code actions, or null.
- Semantic tokens may support full, range, and delta variants.

Flatten only at application boundaries and keep enough metadata for callers to
make correct decisions.

## Concurrency and resilience

- Allow concurrent requests unless the server or library serializes them.
- Avoid blocking the connection read loop in notification handlers.
- Respect context cancellation and deadlines.
- Handle server-initiated requests such as `workspace/applyEdit`,
  `window/showMessageRequest`, and `client/registerCapability`.
- Log unknown notifications at a low level and continue when safe.
- Surface malformed frames, process exits, and unsupported capabilities as clear
  errors.

## Interoperability test matrix

For non-trivial changes, test at least one real language server plus focused fake
servers for edge cases.

| Server | Useful coverage |
| --- | --- |
| gopls | Go workspace symbols, diagnostics, definition, references, semantic tokens |
| pyright | Python diagnostics, workspace configuration, path/URI behavior |
| rust-analyzer | rich semantic tokens, inlay/experimental style features, large workspaces |
| clangd | compile database sensitivity, diagnostics, completion, references |
| typescript-language-server | JS/TS completions, quick fixes, workspace edits |

Record server version, client capabilities, workspace fixture, request payloads
or logs, and the exact assertions. Prefer deterministic fixtures over depending
on a developer's global editor state.
