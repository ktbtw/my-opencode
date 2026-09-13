#!/system/bin/sh

MODDIR=${0%/*}
RUNTIME="$MODDIR/system/bin/frida-runtime/runtime.sh"
until [ "$(getprop sys.boot_completed)" = "1" ]; do
  sleep 2
done
"$RUNTIME" start >/dev/null 2>&1
