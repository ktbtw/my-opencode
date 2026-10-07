# MCP Lazy Loading

## Goal

Replace eager MCP schema expansion with two stable proxy tools so tool-heavy agents stay below provider request-size limits without losing MCP capabilities.

## Tasks

- [x] Add a reusable lazy MCP catalog that exposes `mcp_search` and `mcp_call`.
- [x] Route proxy calls through the existing wrapped MCP tools so permission checks, hooks, truncation, and attachments remain unchanged.
- [x] Filter the hidden catalog with the same agent, session, and per-message rules used for eager tools.
- [x] Keep built-in tools eager and preserve an explicit way to disable the two proxy tools per message.
- [x] Add focused tests for search ranking, schema discovery, invocation, invalid arguments, and compact advertised schemas.
- [x] Run package tests, type checking, a production build, and a real Verify MCP model test.

## Done When

- [x] A Verify-enabled agent advertises only the built-in tools plus two MCP proxy tools.
- [x] The model can discover and execute an arbitrary hidden Verify tool.
- [x] The proxy schemas remain below 2 KB for 75 large hidden tools and the live provider request no longer returns the size-related 502.

## Verification

- Unit tests: lazy MCP `4 pass`; MCP lifecycle `22 pass`; permission/native regression `149 pass` with one unrelated stale recorded-request fixture mismatch.
- Type check: `bun typecheck` exited successfully.
- Production build: Windows x64, macOS arm64, and Linux x64 artifacts were built with `1.15.49`; the macOS binary smoke test reports `1.15.49`.
- Live Agent: 74 hidden Verify tools became 10 built-in tools plus `mcp_search` and `mcp_call` (`tool_count=12`).
- Live call: `mcp_search` discovered `verify_server_info`; `mcp_call` executed it; the model completed with HTTP 200 and no 502.

## Assumptions

- MCP tool names returned by `MCP.tools()` are stable and unique within an instance.
- One additional model round trip for discovery is acceptable for MCP operations.
- Existing MCP permission names remain authoritative; proxy names do not replace them.

## Decision Log

- Chosen: lazy MCP search/call proxies. This removes schema growth as MCP catalogs expand.
- Rejected: alphabetical schema truncation. It can silently hide the tool needed for the current task.
- Rejected: schema minification alone. It remains coupled to provider-specific request limits.
- Rejected: model switching. The same request-shape failure can affect every compatible provider.
