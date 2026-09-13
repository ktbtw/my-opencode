# Launcher Self-Update And Desktop App Update Fix

## Goal
Harden Launcher self-update failure handling and make Flutter clients download and launch the installer for their current platform.

## Tasks
- [ ] Audit Launcher apply lifecycle and Flutter app version/download contracts -> Verify current tests reproduce missing cleanup/rollback/platform selection.
- [ ] Add Launcher update serialization, bounded contexts, persistent apply results, cross-volume replacement, health-confirmed rollback, and artifact cleanup -> Verify focused Go tests.
- [ ] Extend app version metadata and download API with Android APK, Windows EXE, and macOS package assets -> Verify API tests and Range downloads.
- [ ] Update Flutter updater to select the current platform asset, resume and hash downloads, then launch the Windows/macOS installer -> Verify Dart unit tests.
- [ ] Build Windows/macOS Flutter installers and update deployment packaging/version metadata -> Verify installer files and hashes.
- [ ] Run full Launcher, Backend, and Flutter test suites plus Windows real update/install smoke tests.

## Done When
- [ ] Launcher update failures are observable and recoverable without stale artifacts.
- [ ] Windows Flutter downloads and launches the EXE installer; Android continues using APK; macOS uses its desktop package.

