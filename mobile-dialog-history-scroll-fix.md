# Mobile Dialog And History Scroll Fix

## Goal
Keep Agent rename actions horizontal on mobile and preserve the reading anchor while older chat history finishes rendering.

## Tasks
- [x] Replace rename-dialog overflow actions with one responsive row -> Verify all three controls share one Y position at 320px.
- [x] Restore history offsets across delayed layout frames -> Verify incremental height changes retain the reading anchor.
- [x] Run focused widget/unit tests and Flutter analysis -> Verify no failures or diagnostics.

## Done When
- [x] The rename dialog stays single-row on mobile and loading older messages does not jump to the latest message.
