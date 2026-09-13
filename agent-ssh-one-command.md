# Agent SSH One-Command Enrollment

## Goal

Make the generated bootstrap command enroll its own managed SSH key on the bound target Launcher, then establish a reusable master session without manual key setup.

## Tasks

- [x] Add a bootstrap-key endpoint bound to the existing one-time tunnel session.
- [x] Dispatch key installation to the exact target Launcher with a 24-hour expiry.
- [x] Track managed key expiry on the Launcher and remove expired entries safely.
- [x] Generate/reuse a local managed key and force SSH to use it on Unix and Windows.
- [x] Cover token binding, duplicate enrollment, key validation, expiry, and wrapper execution in tests.
- [x] Publish the backend and Launcher, then verify version metadata and generated script behavior online.

## Done When

- [x] A fresh machine with OpenSSH can execute one bootstrap command and receive a working `CHAT_CODEX_SSH_COMMAND` wrapper.
- [x] A bootstrap token can enroll only one public key on its bound target.
- [x] Managed target keys are removed after 24 hours.
