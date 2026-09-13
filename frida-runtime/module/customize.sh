#!/system/bin/sh

ui_print "- Frida Runtime test module"
if [ "$API" -lt 26 ]; then
  abort "Android 8 (API 26) or newer is required"
fi
case "$ARCH" in
  arm64) ABI=arm64-v8a ;;
  arm) ABI=armeabi-v7a ;;
  x86_64) ABI=x86_64 ;;
  x86) ABI=x86 ;;
  *) abort "Unsupported ABI: $ARCH" ;;
esac
SERVER="$MODPATH/system/bin/frida-runtime/$ABI/frida-server"
if [ ! -s "$SERVER" ]; then
  abort "Missing Frida runtime for $ABI"
fi
chmod 0755 "$SERVER"
ui_print "- Selected ABI: $ABI"
