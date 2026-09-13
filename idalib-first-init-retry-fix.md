# IDALib First Initialization Retry Fix

## Goal

Make the managed IDALib first-run probe recover from the observed one-time process exit code 255 while preserving useful diagnostics and strict handling for every other failure.

## Tasks

- [x] Confirm the failed stage and reproduce a successful second import in the same Windows installation.
- [x] Add a single controlled retry for exit code 255 after revalidating the EULA registry state.
- [x] Add unbuffered probe markers and attempt diagnostics.
- [x] Cover retry, non-retry, and preparation failure behavior with Go tests.
- [x] Verify locally and against the Windows IDALib installation.
- [x] Publish Launcher `0.1.130` with `deploy.sh --launcher-only`.

## Done When

- [x] A first probe returning 255 is retried once and can complete.
- [x] Other errors fail immediately with their output preserved.
- [x] Windows imports `idapro`, verifies the EULA value, and completes the IDA smoke test.
