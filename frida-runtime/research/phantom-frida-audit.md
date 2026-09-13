# Phantom Frida Source Audit

Audited source: `TheQmaks/phantom-frida` at commit
`2fbec05193cefa608395f1dec133ad77c89d94aa`.

## Compatibility

- Declared and source-contract-tested target: Frida `17.16.4` on Android.
- Build output: Server and Gadget for Android architectures.
- Required host toolchain: Android NDK r29.
- The builder preserves the standard Frida client protocol, so ordinary
  `frida-tools` clients remain compatible when versions match.

## Observed Change Areas

- Replaces selected static and runtime implementation identifiers in server,
  gadget, helper, agent, and generated helper DEX components.
- Changes selected implementation-visible thread, local resource, and default
  transport naming values.
- Provides a build-time artifact scan that rejects a fixed set of known marker
  strings before output promotion.
- Includes an optional strict W^X allocation mode intended to reduce persistent
  writable-and-executable Frida-owned mappings.
- Provides rooted-device smoke tests for server startup, RPC, spawn, attach,
  Java bridge behavior, and external mapping inspection.

## Limits Confirmed By Source Documentation

- Absence of known strings is not proof against behavior, timing, integrity, or
  application-specific checks.
- It does not cover root state, user-script content, active instrumentation
  behavior, remote attestation, or application-defined integrity checks.
- Its Android evidence is tied to the stated Frida version and a tested device;
  Android 8-17 and every ABI require the project-specific matrix to be run.

## Integration Status

The current `frida-runtime-test.zip` remains the standard Frida build used to
validate module packaging, manager installation, startup, and host RPC. This
audit source is kept separately so a new build cannot overwrite the validated
baseline. Promotion requires a clean four-ABI build and the same module/device
acceptance matrix.
