# Frida Runtime Test Module Plan

- [x] Add a reproducible multi-ABI artifact manifest and validation utility.
- [x] Add a universal Android root-module layout with runtime profiles.
- [x] Add service and action scripts for start, stop, restart, and status.
- [x] Add an ADB installer that detects supported root-manager engines.
- [ ] Add host and device readiness checks with structured JSON output.
- [x] Add unit tests for installer command selection and module validation.
- [x] Build the test ZIP and run shell and Go test suites locally.
- [x] Audit the pinned Phantom Frida source and record its declared compatibility and limits.

This test release intentionally has no process-level filtering or detection-evasion layer. It packages and operates an explicitly selected Frida server artifact, validates integrity, and reports readiness.
