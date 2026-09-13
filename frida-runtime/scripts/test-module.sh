#!/usr/bin/env sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
for script in "$ROOT/module/customize.sh" "$ROOT/module/service.sh" "$ROOT/module/action.sh" "$ROOT/module/system/bin/frida-runtime/runtime.sh" "$ROOT/scripts/build-frida.sh" "$ROOT/scripts/build-module.sh" "$ROOT/scripts/install-module.sh"; do
  sh -n "$script"
done
for abi in arm64-v8a armeabi-v7a x86 x86_64; do
  grep -q "\"$abi\"" "$ROOT/artifacts/manifest.json"
done
if ! grep -q 'command "\$ADB"' "$ROOT/scripts/install-module.sh"; then
  echo "installer must invoke the adb executable directly" >&2
  exit 1
fi
echo "module shell checks passed"
