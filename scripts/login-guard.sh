#!/bin/bash
set -Eeuo pipefail

JAIL_NAME="novapanel"
ROOT="${NOVAS_LOGIN_GUARD_ROOT:-}"
FILTER_DIR="${ROOT}/etc/fail2ban/filter.d"
JAIL_DIR="${ROOT}/etc/fail2ban/jail.d"
FILTER_FILE="${FILTER_DIR}/${JAIL_NAME}.conf"
JAIL_FILE="${JAIL_DIR}/${JAIL_NAME}.local"
FAIL2BAN_CLIENT="${NOVAS_FAIL2BAN_CLIENT:-fail2ban-client}"
SYSTEMCTL="${NOVAS_SYSTEMCTL:-systemctl}"

require_root() {
    if [[ -z "$ROOT" && ${EUID:-$(id -u)} -ne 0 ]]; then
        echo "login protection requires root" >&2
        exit 1
    fi
}

atomic_write() {
    local target="$1" content="$2" temporary
    mkdir -p "$(dirname "$target")"
    temporary=$(mktemp "$(dirname "$target")/.novapanel.XXXXXX")
    printf '%s\n' "$content" > "$temporary"
    chmod 0644 "$temporary"
    mv -f "$temporary" "$target"
}

select_banaction() {
    # Fail2ban's stock ufw action blocks the source from every destination
    # port. Multiport actions coexist with UFW while isolating the ban to the
    # NovaPanel TCP port, so an administrative SSH session cannot be blocked.
    if [[ -f "${ROOT}/etc/fail2ban/action.d/nftables-multiport.conf" ]] && command -v nft >/dev/null 2>&1; then
        printf '%s' 'nftables-multiport'
        return
    fi
    if [[ -f "${ROOT}/etc/fail2ban/action.d/iptables-multiport.conf" ]] && command -v iptables >/dev/null 2>&1; then
        printf '%s' 'iptables-multiport'
        return
    fi
    echo "no supported Fail2ban multiport action is available" >&2
    return 1
}

sync_guard() {
    local port="$1" ignore_list="${2:-}" action filter_config jail_config
    [[ "$port" =~ ^[0-9]+$ ]] && ((port >= 1 && port <= 65535)) || {
        echo "invalid panel port: $port" >&2
        exit 1
    }
    command -v "$FAIL2BAN_CLIENT" >/dev/null 2>&1 || {
        echo "fail2ban-client is not installed" >&2
        exit 1
    }
    if [[ "$ignore_list" == *$'\n'* || "$ignore_list" == *$'\r'* || ! "$ignore_list" =~ ^[0-9A-Fa-f:./[:space:]]*$ ]]; then
        echo "invalid login protection allowlist" >&2
        exit 1
    fi
    action=$(select_banaction)
    filter_config='[Definition]
failregex = ^(?:WARNING - )?NOVAS_LOGIN_FAILED remote_ip=<HOST>$
ignoreregex =
journalmatch = _SYSTEMD_UNIT=novas.service'
    jail_config="[${JAIL_NAME}]
enabled = true
filter = ${JAIL_NAME}
backend = systemd
port = ${port}
protocol = tcp
maxretry = 10
findtime = 10m
bantime = -1
usedns = no
ignoreip = 127.0.0.1/8 ::1${ignore_list:+ ${ignore_list}}
banaction = ${action}"

    atomic_write "$FILTER_FILE" "$filter_config"
    atomic_write "$JAIL_FILE" "$jail_config"
    "$FAIL2BAN_CLIENT" -t >/dev/null
    "$SYSTEMCTL" enable --now fail2ban >/dev/null
    if ! "$FAIL2BAN_CLIENT" reload >/dev/null 2>&1; then
        "$SYSTEMCTL" restart fail2ban
    fi
    "$FAIL2BAN_CLIENT" status "$JAIL_NAME" >/dev/null
}

remove_guard() {
    if command -v "$FAIL2BAN_CLIENT" >/dev/null 2>&1; then
        "$FAIL2BAN_CLIENT" stop "$JAIL_NAME" >/dev/null 2>&1 || true
    fi
    rm -f "$FILTER_FILE" "$JAIL_FILE"
    if command -v "$FAIL2BAN_CLIENT" >/dev/null 2>&1; then
        "$FAIL2BAN_CLIENT" reload >/dev/null 2>&1 || true
    fi
}

require_root
case "${1:-}" in
sync)
    [[ $# -ge 2 && $# -le 3 ]] || { echo "usage: $0 sync PORT [IGNORE_LIST]" >&2; exit 2; }
    sync_guard "$2" "${3:-}"
    ;;
remove)
    [[ $# -eq 1 ]] || { echo "usage: $0 remove" >&2; exit 2; }
    remove_guard
    ;;
*)
    echo "usage: $0 {sync PORT [IGNORE_LIST]|remove}" >&2
    exit 2
    ;;
esac
