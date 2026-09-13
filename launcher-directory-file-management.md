# Launcher Directory File Management

## Goal

Add file upload, empty-file creation, folder creation, and recursive deletion to the device directory page without requiring an Agent project.

## Confirmed Scope

- The target is Flutter's device directory page used while browsing Launcher directories.
- Upload local files, create empty files, create folders, and delete selected files or folders.
- Keep Agent project-file APIs unchanged.
- Use global notifications for operation progress and results.
- Support Windows, macOS, and Linux with the same protocol.

## Design

Flutter sends directory mutations through device-scoped HTTP endpoints. The backend forwards each request through the existing request/result relay channel. Launcher implements device-directory mutations separately from Agent project mutations and checks the current directory access policy on every request.

Uploads use the existing chunked transfer shape: create a target-bound temporary upload, send indexed chunks, verify per-chunk and whole-file SHA-256 values, then atomically publish the completed file without replacing an existing target. Directory mutations are serialized by a dedicated Launcher lock so completion cannot race chunk writes or another mutation. Creation rejects existing targets. Deletion rejects a discovered root and recursively removes confirmed folders. Every path is cleaned, checked against the active allowed roots, and resolved through existing ancestors to prevent symbolic-link traversal outside the allowed boundary.

The device directory page adds upload and create controls plus long-press multi-selection. Deletion always presents a confirmation dialog, with stronger copy when folders are selected. The current directory reloads after successful mutations.

## Assumptions

- A user with `allow_all_directories` enabled may mutate any non-root path accessible to the Launcher process.
- Hidden entries remain excluded from browsing and cannot be created through the UI.
- Uploads do not overwrite existing files.
- Batch deletion is sequential and stops on the first failure so the UI can reload the authoritative directory state.

## Decision Log

- Chose dedicated `device.directory_files.*` operations instead of reusing `device.project_files.*` because device directory management must not require an Agent or project root.
- Chose chunked uploads with atomic completion to bound memory usage and avoid partial destination files.
- Chose recursive folder deletion with explicit confirmation because the requested workflow includes deleting folders.
- Chose server and Launcher validation on every operation rather than relying on Flutter path state.

## Tasks

- [x] Implement Launcher path validation, create, delete, and chunked upload operations.
- [x] Add relay message handlers and backend device-directory HTTP routes.
- [x] Add Flutter repository methods and device directory management controls.
- [x] Add Launcher, relay, backend, and Flutter tests.
- [x] Run Go tests, Flutter tests, static analysis, and Web release build.

## Done When

- [x] Device directory operations work without an Agent ID.
- [x] Root, traversal, symlink escape, overwrite, and upload-integrity cases are rejected.
- [x] Existing Agent project-file behavior remains unchanged.
