# Windows Launcher Locked EXE Bootstrap Fix

## Goal
Prevent GUI bootstrap and autostart migration from overwriting a running Windows background Launcher executable.

## Tasks
- [x] Separate stable executable path resolution from binary synchronization in `launcher/gui/app.go`.
- [x] Stop an unresponsive process holding the configured control port and wait for exit before fallback installation.
- [x] Keep autostart migration path-only when an installed executable already exists.
- [x] Add regression tests for unchanged binaries, autostart path resolution, and stale-process recovery helpers.
- [x] Bump Launcher to `0.1.121`, build release artifacts, and deploy with `./deploy.sh --launcher-only`.

## Done When
- [x] Launcher GUI tests and Windows cross-build tests pass.
- [x] Published Launcher metadata and artifacts report `0.1.121`.
