# Windows Launcher Console Flash Fix

## Goal
Prevent periodic Windows console flashes from launcher autostart registry checks and publish launcher 0.1.109.

## Tasks
- [x] Run all `reg.exe` autostart operations with `CREATE_NO_WINDOW` and `HideWindow`.
- [x] Add and run regression tests locally and on the Windows LAN host.
- [x] Build launcher 0.1.109 artifacts and deploy them through the existing launcher release flow.
- [x] Verify hosted metadata, downloads, and the updated Windows launcher behavior.

## Done When
- [x] Launcher 0.1.109 is hosted and periodic autostart polling no longer creates a visible console window.
