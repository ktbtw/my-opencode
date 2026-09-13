#!/system/bin/sh

MODDIR=${0%/*/*/*/*}
STATE_DIR=/data/adb/frida-runtime
CONFIG="$STATE_DIR/config.conf"
PIDFILE="$STATE_DIR/frida-server.pid"
LOGFILE="$STATE_DIR/frida-server.log"
mkdir -p "$STATE_DIR"
chmod 0700 "$STATE_DIR"

api=$(getprop ro.build.version.sdk)
abi=$(getprop ro.product.cpu.abi)
case "$abi" in
  arm64-v8a|armeabi-v7a|x86|x86_64) ;;
  *) echo "unsupported ABI: $abi"; exit 2 ;;
esac

profile=standard
if [ "$api" -ge 26 ] && [ "$api" -le 28 ]; then
  profile=compat
elif [ "$api" -ge 35 ]; then
  profile=modern
fi

port=27042
listen=127.0.0.1
[ -f "$CONFIG" ] && . "$CONFIG"
server="$MODDIR/system/bin/frida-runtime/$abi/frida-server"
[ -x "$server" ] || { echo "missing executable: $server"; exit 3; }

is_running() {
  [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null
}
start() {
  if is_running; then
    echo "running pid=$(cat "$PIDFILE") profile=$profile port=$port"
    return 0
  fi
  rm -f "$PIDFILE"
  "$server" -l "$listen:$port" >>"$LOGFILE" 2>&1 &
  echo $! > "$PIDFILE"
  sleep 1
  is_running || { echo "start failed; see $LOGFILE"; return 1; }
  echo "started pid=$(cat "$PIDFILE") profile=$profile port=$port"
}
stop() {
  if is_running; then
    kill "$(cat "$PIDFILE")" 2>/dev/null || true
  fi
  rm -f "$PIDFILE"
  echo "stopped"
}
status() {
  is_running && { echo "running pid=$(cat "$PIDFILE") profile=$profile port=$port"; return 0; }
  echo "stopped profile=$profile port=$port"
  return 1
}
case "${1:-}" in
  start) start ;;
  stop) stop ;;
  restart) stop; start ;;
  status) status ;;
  *) echo "usage: $0 {start|stop|restart|status}"; exit 64 ;;
esac
