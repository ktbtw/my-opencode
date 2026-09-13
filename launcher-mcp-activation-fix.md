# Launcher MCP Activation Fix

## Goal
Ensure every MCP prepared for a semantic Agent is enabled in that Agent runtime, and report readiness failures on the actual failing MCP only.

## Tasks
- [x] Add a regression test for mixed built-in and dynamically installed MCP recommendations.
- [x] Merge resolved MCP configs with all globally available recommended MCP names.
- [x] Attribute readiness errors per MCP instead of copying one aggregate error to every item.
- [x] Run focused Launcher tests and the full Launcher test suite.
- [x] Bump, build, publish, and verify the Launcher release.

## Done When
- [x] A semantic Agent can start with `verify` plus dynamically installed `idalib-mcp`, while unrelated MCPs remain disabled.
- [x] Only the MCP that failed readiness is shown as failed.
- [x] Published Launcher metadata and artifact hashes pass deployment checks.
