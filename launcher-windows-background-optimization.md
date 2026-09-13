# Windows Launcher Background Optimization

## Goal
Stop repeated registry reads during status polling and keep launcher updates fully windowless on Windows.

## Tasks
- [x] Cache autostart status after the first read and refresh it only on mutation or explicit request.
- [x] Apply the shared Windows no-window process configuration to update and initialization commands.
- [x] Add local and Windows regression coverage for caching and process creation flags.
- [x] Build, deploy, and exercise launcher 0.1.112 on the Windows LAN host while sampling child processes.

## Done When
- [x] Polling performs no repeated `reg.exe` reads, and an update creates no visible console window.
