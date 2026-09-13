#!/usr/bin/env sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
SOURCE="$ROOT/upstream/frida"
BUILD_ROOT="$ROOT/build"
ARTIFACTS="$ROOT/artifacts"
NDK_DEFAULT="$HOME/Library/Android/sdk/ndk/29.0.13846066"
NDK_ROOT=${ANDROID_NDK_ROOT:-$NDK_DEFAULT}
JOBS=${JOBS:-$(sysctl -n hw.ncpu 2>/dev/null || echo 4)}

if [ "$#" -eq 0 ]; then
  set -- arm64-v8a armeabi-v7a x86 x86_64
fi

[ -x "$SOURCE/configure" ] || { echo "Frida source is missing: $SOURCE" >&2; exit 1; }
[ -f "$NDK_ROOT/source.properties" ] || { echo "Android NDK r29 is missing: $NDK_ROOT" >&2; exit 1; }
grep -q 'Pkg.Revision = 29\.' "$NDK_ROOT/source.properties" || { echo "Android NDK r29 is required" >&2; exit 1; }
export ANDROID_NDK_ROOT="$NDK_ROOT"

for abi in "$@"; do
  case "$abi" in
    arm64-v8a) host=arm64 ;;
    armeabi-v7a) host=arm ;;
    x86) host=x86 ;;
    x86_64) host=x86_64 ;;
    *) echo "unsupported ABI: $abi" >&2; exit 64 ;;
  esac

  build="$BUILD_ROOT/android-$host"
  prefix="$build/prefix"
  rm -rf "$build"
  mkdir -p "$build"
  (
    cd "$build"
    "$SOURCE/configure" \
      "--prefix=$prefix" \
      "--host=android-$host" \
      --enable-portal \
      -- \
      -Dfrida-gum:devkits=gum,gumjs \
      -Dfrida-core:compiler_backend=enabled \
      -Dfrida-core:devkits=core
    make -j"$JOBS"
    make install
  )
  server="$prefix/bin/frida-server"
  [ -x "$server" ] || { echo "missing build output: $server" >&2; exit 1; }
  cp "$server" "$ARTIFACTS/frida-server-$abi"
  chmod 0755 "$ARTIFACTS/frida-server-$abi"
done

"$ROOT/scripts/update-manifest.sh"
