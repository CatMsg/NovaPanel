#!/bin/sh
set -e

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
cd "$SCRIPT_DIR"

PID_FILE="$SCRIPT_DIR/.runSUI.pid"
LOG_FILE="$SCRIPT_DIR/.runSUI.log"
TAIL_PID=""
APP_PID=""

read_pid_file() {
  if [ -f "$PID_FILE" ]; then
    cat "$PID_FILE"
  fi
}

cleanup_start() {
  if [ -n "$TAIL_PID" ] && kill "$TAIL_PID" >/dev/null 2>&1; then
    wait "$TAIL_PID" >/dev/null 2>&1 || true
  fi
  if [ -n "$APP_PID" ] && kill "$APP_PID" >/dev/null 2>&1; then
    wait "$APP_PID" >/dev/null 2>&1 || true
  fi
  rm -f "$PID_FILE"
}

running_pid() {
  pid="$(read_pid_file 2>/dev/null || true)"
  if [ -n "$pid" ] && ps -p "$pid" >/dev/null 2>&1; then
    printf '%s\n' "$pid"
    return 0
  fi

  if [ -f "$PID_FILE" ]; then
    rm -f "$PID_FILE"
  fi

  pid="$(pgrep -x sui 2>/dev/null | head -n 1 || true)"
  if [ -n "$pid" ] && ps -p "$pid" >/dev/null 2>&1; then
    printf '%s\n' "$pid"
    return 0
  fi

  return 1
}

stop_existing_sui() {
  pids="$(pgrep -x sui 2>/dev/null || true)"
  if [ -z "$pids" ]; then
    pid="$(running_pid 2>/dev/null || true)"
    if [ -z "$pid" ]; then
      return 0
    fi
    pids="$pid"
  fi

  for pid in $pids; do
    kill "$pid" >/dev/null 2>&1 || true
  done

  attempt=0
  while pgrep -x sui >/dev/null 2>&1; do
    if [ "$attempt" -ge 5 ]; then
      for pid in $(pgrep -x sui 2>/dev/null || true); do
        kill -9 "$pid" >/dev/null 2>&1 || true
      done
    fi
    sleep 1
    attempt=$((attempt + 1))
  done
  rm -f "$PID_FILE"
}

status_existing_sui() {
  pid="$(running_pid 2>/dev/null || true)"
  if [ -z "$pid" ]; then
    return 1
  fi

  echo "sui running:"
  ps -p "$pid" -o pid=,command=
  echo "pid file: $PID_FILE"
  echo "log file: $LOG_FILE"
  return 0
}

start_sui() {
  sh "$SCRIPT_DIR/build.sh"
  : > "$LOG_FILE"
  env SUI_DB_FOLDER="db" SUI_DEBUG=true ./sui >>"$LOG_FILE" 2>&1 &
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
    stop_existing_sui
    start_sui
    ;;
  restart)
    stop_existing_sui
    start_sui
    ;;
  stop)
    stop_existing_sui
    ;;
  status)
    if status_existing_sui; then
      exit 0
    fi
    echo "sui not running"
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
Usage: sh runSUI.sh [run|start|stop|restart|status|logs|help]
  run/start  build then start and follow logs
  stop       stop the current sui process
  restart    stop, build, then start
  status     show the current sui process and paths
  logs       show the last 200 log lines, or use -f to follow
EOF
    ;;
  *)
    echo "unknown command: ${cmd}" >&2
    echo "Usage: sh runSUI.sh [run|start|stop|restart|status|logs|help]" >&2
    exit 2
    ;;
esac
