#!/usr/bin/env sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
ZIP=${FRIDA_RUNTIME_ZIP:-$ROOT/dist/frida-runtime-test.zip}
SERIAL=${1:-}
ADB=${ADB:-adb}

[ -n "$SERIAL" ] || { echo "usage: $0 DEVICE_SERIAL" >&2; exit 64; }
[ -f "$ZIP" ] || { echo "module ZIP not found: $ZIP" >&2; exit 1; }

adb() { command "$ADB" -s "$SERIAL" "$@"; }
root() { adb shell "su -c '$1'"; }

root_identity=$(root id)
case "$root_identity" in
  *uid=0*) ;;
  *) echo "root shell is required" >&2; exit 1 ;;
esac
api=$(adb shell getprop ro.build.version.sdk | tr -d '\r\n')
abi=$(adb shell getprop ro.product.cpu.abi | tr -d '\r\n')
case "$api" in ''|*[!0-9]*) echo "invalid Android API: $api" >&2; exit 1;; esac
[ "$api" -ge 26 ] && [ "$api" -le 37 ] || { echo "Android API 26-37 is required (got $api)" >&2; exit 1; }
case "$abi" in arm64-v8a|armeabi-v7a|x86|x86_64) ;; *) echo "unsupported ABI: $abi" >&2; exit 1;; esac

remote=/data/local/tmp/frida-runtime-test.zip
adb push "$ZIP" "$remote" >/dev/null
cleanup() { root "rm -f $remote" >/dev/null 2>&1 || true; }
trap cleanup EXIT

if root 'test -x /data/adb/apd'; then
  root "/data/adb/apd module install $remote"
elif root 'test -x /data/adb/ksud'; then
  root "/data/adb/ksud module install $remote"
elif root 'command -v magisk >/dev/null 2>&1'; then
  root "magisk --install-module $remote"
else
  echo "no supported root-manager module engine found" >&2
  exit 1
fi

module_root=/data/adb/modules/frida_runtime
[ -x "$module_root/system/bin/frida-runtime/runtime.sh" ] || module_root=/data/adb/modules_update/frida_runtime
root "$module_root/system/bin/frida-runtime/runtime.sh start" || true
echo "installed serial=$SERIAL api=$api abi=$abi"
