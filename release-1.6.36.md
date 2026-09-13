# Release 1.6.36

## Goal

Publish the model thinking-variant precedence fix as app version `1.6.36+183`, CLI version `1.15.79`, and Launcher version `0.1.170`.

## Changes

- Explicit model variants now replace automatically inferred variants instead of being merged.
- Manual thinking-variant overrides remain authoritative over runtime provider metadata.
- `stealth/ox-alpha` defaults to the supported low/high variants only.

## Verification

- OpenCode provider tests: 90 passed.
- Launcher AI configuration tests passed.
- Android APK, Windows installer, macOS DMG, CLI, and Launcher artifacts built and deployed.
- Production download SHA-256 checks passed for all published artifacts.
