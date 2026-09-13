# Model Discovery And Dialog Loading Release

## Goal

Release model discovery fixes, dialog skeleton loading, and accumulated client, CLI, Launcher, and backend updates.

## Tasks

- [x] Remove the backend fallback for older Launcher model payloads.
- [x] Bump client, Launcher, and CLI versions and refresh release notes.
- [x] Run Flutter, Go, and CLI release checks.
- [x] Build the Windows client installer on the Windows LAN builder.
- [x] Build Backend, Flutter Web/APK/macOS, Admin Web, CLI, and Launcher artifacts locally.
- [x] Deploy application artifacts and managed runtimes with `deploy.sh all` component modes.
- [x] Verify production health, version metadata, artifact hashes, and download ranges.

## Validation Notes

- Backend, Launcher, Flutter, CLI typecheck, release metadata, shell syntax, and deploy dry-run checks passed.
- CLI release-focused suite passed 121/121 tests with single-test concurrency.
- The broad CLI suite passed 2955 tests and retained 17 reproducible failures in legacy instance reload, JSON Todo migration, recorded-request fixture, timestamp cleanup, and JSON stdout contracts.

## Done When

- [x] Production reports client `1.6.30+177`, Launcher `0.1.167`, and CLI `1.15.76`.
- [x] Backend is active and all public health/download checks pass.
