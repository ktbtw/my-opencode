# Frida Runtime Android Module

Test module for running a selected Frida server binary on rooted Android 8-17 devices.
It supports the module engines used by Magisk, Magisk Alpha, Kitsune Mask,
KernelSU, KernelSU Next, SukiSU Ultra, and APatch.

## Profiles

- `compat`: Android API 26-28
- `standard`: Android API 29-34
- `modern`: Android API 35-37

## Build a test ZIP

Place a verified server executable for every ABI below `artifacts/`, update its
SHA-256 in `artifacts/manifest.json`, then run:

```sh
./scripts/build-module.sh
```

The test suite uses synthetic artifacts only. A real device ZIP requires every
artifact to be present and hash-verified before packaging.

## Build from source

The pinned Frida source checkout is in `upstream/frida`; its exact revision is
recorded in `source.lock.json`. Android NDK r29 is required. Build all ABIs:

```sh
./scripts/build-frida.sh
./scripts/build-module.sh
```

Install the generated ZIP to an already-connected root device:

```sh
./scripts/install-module.sh DEVICE_SERIAL
```
