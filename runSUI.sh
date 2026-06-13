#!/bin/sh
set -e

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
cd "$SCRIPT_DIR"

stop_existing_sui() {
  if pgrep -x sui >/dev/null 2>&1; then
    pkill -x sui >/dev/null 2>&1 || true
    attempt=0
    while pgrep -x sui >/dev/null 2>&1; do
      if [ "$attempt" -ge 5 ]; then
        pkill -9 -x sui >/dev/null 2>&1 || true
      fi
      sleep 1
      attempt=$((attempt + 1))
    done
  fi
}

status_existing_sui() {
  pids="$(pgrep -x sui 2>/dev/null || true)"
  if [ -z "${pids}" ]; then
    return 1
  fi

  echo "sui running:"
  for pid in ${pids}; do
    ps -p "${pid}" -o pid=,command=
  done
  return 0
}

start_sui() {
  sh "$SCRIPT_DIR/build.sh"
  exec env SUI_DB_FOLDER="db" SUI_DEBUG=true ./sui
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
  help|-h|--help)
    cat <<'EOF'
Usage: sh runSUI.sh [run|start|stop|restart|status|help]
EOF
    ;;
  *)
    echo "unknown command: ${cmd}" >&2
    echo "Usage: sh runSUI.sh [run|start|stop|restart|status|help]" >&2
    exit 2
    ;;
esac
