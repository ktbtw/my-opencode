# Flutter Windows Installer

## Goal
Build the current `flutter_app` as a Windows x64 release and package it as a single EXE installer.

## Tasks
- [x] Confirm the remote Windows host has Flutter, Visual Studio C++, and Windows SDK.
- [x] Generate the Flutter `windows/` runner and set product metadata/icon.
- [x] Add an Inno Setup definition that bundles the complete release directory.
- [x] Sync the Flutter project to `192.168.10.105` and run the release build.
- [x] Build the installer, copy it back to the local release directory, and verify metadata/hash.

## Done When
- [x] A Windows x64 setup EXE exists locally and launches successfully on the Windows host.

## Notes
- Product version: `1.6.4` (`pubspec.yaml` build `151`).
- Default API base URL stays aligned with the existing deployment value.
- Installer SHA-256: `4c759b2b3ceee8fdf332540acb29bb5c6b43f98b20562f9aa0f49f5617d466e7`.
