---
name: lsp
description: "Use when Codex needs to design, implement, debug, test, or review Language Server Protocol (LSP) clients, servers, middleware, SDK bindings, or editor integrations, including JSON-RPC framing, initialize/shutdown lifecycle, capability negotiation, textDocument/workspace methods, diagnostics, hover, completion, definition, references, formatting, semantic tokens, code actions, file watching, dynamic registration, position/range encoding, URI handling, cancellation, progress, or interoperability with language servers such as gopls, pyright, clangd, rust-analyzer, or TypeScript language services."
---

# Language Server Protocol

## Overview

Use this skill to work on LSP integrations without mixing editor-facing behavior,
protocol transport, generated protocol types, and language-server process
lifecycle. Favor exact protocol evidence over remembered shapes whenever a wire
method, capability, or option matters.

## Core workflow

1. Classify the LSP role and feature.
   - Identify whether the code is an LSP client, server, proxy, test harness,
     protocol type package, editor adapter, or MCP/tool bridge.
   - Name the exact methods and notifications involved, such as
     `initialize`, `textDocument/definition`, `textDocument/publishDiagnostics`,
     or `workspace/applyEdit`.
2. Ground the work in the local implementation.
   - Inspect existing protocol libraries, generated types, transport wrappers,
     session lifecycle code, and feature siblings before adding new shapes.
   - Reuse upstream/generated protocol types when available; avoid hand-rolled
     wire structs unless the existing library cannot express a union.
   - Keep package boundaries explicit: transport/lifecycle belongs near LSP
     client/server code; user-facing formatting and validation belongs near the
     caller/tool layer.
3. Verify the protocol contract.
   - Read `references/lsp-reference.md` for checklists and source links.
   - For exact fields, capabilities, registration options, or version-sensitive
     behavior, check the official LSP specification or meta model for the target
     version before implementing.
   - Treat the latest spec version as time-sensitive; verify it when the user
     asks for current behavior or a protocol upgrade.
4. Trace the message sequence.
   - Write the happy path and failure path as ordered JSON-RPC messages.
   - Include capability gates, dynamic registration, cancellation, partial
     results, work-done progress, and server/client initiated requests when the
     feature can use them.
5. Implement at the right boundary.
   - Keep LSP wire positions zero-based and encoded according to negotiated
     position encoding; convert to user-facing coordinates only at UI/tool
     boundaries.
   - Preserve URI semantics instead of assuming local filesystem paths.
   - Keep stdio transport clean: do not write logs or progress to stdout when
     stdout carries LSP or another framed protocol.
   - Make teardown deterministic: close JSON-RPC connections, subprocess pipes,
     timers, goroutines, file watchers, and caches on cancellation and failure.
6. Validate interoperability.
   - Add unit tests for conversion, capability gating, request/response shape,
     and lifecycle edge cases.
   - Add integration or golden tests against real language servers when behavior
     depends on server quirks or negotiated capabilities.
   - Exercise negative paths: malformed frames, missing capabilities, cancelled
     requests, server exit, partial responses, URI normalization, and concurrent
     notifications.

## Task patterns

| Task | Focus first | Typical validation |
| --- | --- | --- |
| New language feature | Capability gate, request params, result union, conversion boundary | Unit test conversion plus integration against one real server |
| Diagnostics | Push vs pull mode, versioning, cache lifetime, non-blocking notification path | Publish/pull tests plus stale diagnostic clearing |
| Definition/references/hover | Position encoding, URI to path mapping, result flattening | Boundary tests for line/column and multi-location responses |
| Completion/signature help | Trigger characters, resolve support, item defaults, snippets | Capability-matrix tests and realistic editor/server fixture |
| Formatting/code actions | Workspace edits, change annotations, snippet support, document versions | Golden edit application and capability fallback tests |
| Lifecycle/transport | Initialize ordering, shutdown/exit, process IO, log isolation | Fake server tests plus real-server smoke test |
| Protocol upgrade | Changelog, meta model delta, library/generated type drift | Versioned fixture tests and compatibility notes |

## Reference loading

Load `references/lsp-reference.md` when any of these are true:

- The task involves a concrete LSP method, capability, registration option, or
  lifecycle sequence.
- The work crosses JSON-RPC, subprocess, stdio, editor, or MCP/tool boundaries.
- A bug may involve coordinate encoding, stale diagnostics, server shutdown,
  URI conversion, dynamic registration, cancellation, or partial results.
- The user asks for latest/current LSP behavior or a protocol-version upgrade.

## Design alternatives

Prefer the smallest option that satisfies interoperability requirements:

1. Use existing LSP library/generated types.
   - Best for correctness and future upgrades.
   - Risk: library may lag the latest spec or expose awkward union types.
2. Add a narrow adapter around the existing protocol package.
   - Best when caller-facing ergonomics need different names or coordinate
     conventions.
   - Risk: adapter drift if it duplicates too much protocol knowledge.
3. Implement raw JSON-RPC shapes locally.
   - Use only for unsupported or experimental protocol areas.
   - Risk: high maintenance cost; add tests from official examples/meta model.

## Completion criteria

Before claiming completion, provide evidence for:

- Local code paths and protocol types inspected.
- Exact methods/capabilities implemented or diagnosed.
- Coordinate, URI, lifecycle, and cleanup behavior covered.
- Tests or smoke checks run, with explicit gaps if real server validation was not
  possible.
