#!/bin/sh
set -e

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
cd "$SCRIPT_DIR"

PID_FILE="$SCRIPT_DIR/.runNovas.pid"
LOG_FILE="$SCRIPT_DIR/.runNovas.log"
TAIL_PID=""
APP_PID=""

cleanup_start() {
  if [ -n "$TAIL_PID" ] && kill "$TAIL_PID" >/dev/null 2>&1; then
    wait "$TAIL_PID" >/dev/null 2>&1 || true
  fi
  if [ -n "$APP_PID" ] && kill "$APP_PID" >/dev/null 2>&1; then
    wait "$APP_PID" >/dev/null 2>&1 || true
  fi
  rm -f "$PID_FILE"
}

owned_pids() {
  for pid in $(pgrep -x novas 2>/dev/null || true); do
    case "$pid" in *[!0-9]*|'') continue ;; esac
    if [ -d "/proc/$pid/cwd" ]; then
      process_dir=$(readlink "/proc/$pid/cwd" 2>/dev/null || true)
    else
      process_dir=$(lsof -a -p "$pid" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p' || true)
    fi
    [ "$process_dir" != "$SCRIPT_DIR" ] || printf '%s\n' "$pid"
  done
}

running_pid() {
  pid=$(owned_pids | head -n 1)
  [ -n "$pid" ] || return 1
  printf '%s\n' "$pid"
}

stop_existing_novas() {
  pids=$(owned_pids)
  for pid in $pids; do kill "$pid" 2>/dev/null || true; done
  attempt=0
  while [ -n "$(owned_pids)" ]; do
    if [ "$attempt" -ge 30 ]; then
      echo "Panel did not stop cleanly; refusing to rebuild a running panel" >&2
      return 1
    fi
    sleep 1
    attempt=$((attempt + 1))
  done
  rm -f "$PID_FILE"
}

status_existing_novas() {
  pid="$(running_pid 2>/dev/null || true)"
  if [ -z "$pid" ]; then
    return 1
  fi

  echo "novas running:"
  ps -p "$pid" -o pid=,command=
  echo "pid file: $PID_FILE"
  echo "log file: $LOG_FILE"
  return 0
}

start_novas() {
  sh "$SCRIPT_DIR/build.sh"
  : > "$LOG_FILE"
  env NOVAS_DB_FOLDER="db" NOVAS_DEBUG=true ./novas >>"$LOG_FILE" 2>&1 &
  APP_PID=$!
  printf '%s\n' "$APP_PID" > "$PID_FILE"
  tail -f "$LOG_FILE" &
  TAIL_PID=$!
  trap 'cleanup_start; exit 130' INT TERM
  trap 'cleanup_start' EXIT
  if wait "$APP_PID"; then
    exit_code=0
  else
    exit_code=$?
  fi
  trap - INT TERM
  cleanup_start
  exit "$exit_code"
}

cmd="${1:-run}"

case "${cmd}" in
  run|start)
    stop_existing_novas
    start_novas
    ;;
  restart)
    stop_existing_novas
    start_novas
    ;;
  stop)
    stop_existing_novas
    ;;
  status)
    if status_existing_novas; then
      exit 0
    fi
    echo "novas not running"
    exit 1
    ;;
  logs)
    if [ ! -f "$LOG_FILE" ]; then
      echo "log file not found: $LOG_FILE" >&2
      exit 1
    fi
    if [ "${2:-}" = "-f" ] || [ "${2:-}" = "--follow" ]; then
      tail -f "$LOG_FILE"
    else
      tail -n 200 "$LOG_FILE"
    fi
    ;;
  help|-h|--help)
    cat <<EOF
Usage: sh runNovas.sh [run|start|stop|restart|status|logs|help]
  run/start  build then start and follow logs
  stop       stop the current novas process
  restart    stop, build, then start
  status     show the current novas process and paths
  logs       show the last 200 log lines, or use -f to follow
EOF
    ;;
  *)
    echo "unknown command: ${cmd}" >&2
    echo "Usage: sh runNovas.sh [run|start|stop|restart|status|logs|help]" >&2
    exit 2
    ;;
esac
