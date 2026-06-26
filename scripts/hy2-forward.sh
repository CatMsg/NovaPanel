#!/usr/bin/env bash
set -euo pipefail

action="${1:-}"
tag="${2:-}"
listen_port="${3:-}"
ports_csv="${4:-}"
backend_override="${HY2_FORWARD_BACKEND:-auto}"

if [[ -z "${action}" ]]; then
  echo "usage: $0 <apply|remove|purge> [tag] [listen_port] [ports_csv]" >&2
  exit 1
fi

case "${action}" in
  apply|remove)
    if [[ -z "${tag}" ]]; then
      echo "usage: $0 <${action}> <tag> <listen_port> <ports_csv>" >&2
      exit 1
    fi
    ;;
  purge)
    ;;
  *)
    echo "invalid action: ${action}" >&2
    exit 1
    ;;
esac

if [[ "${action}" == "apply" ]]; then
  if [[ -z "${listen_port}" || ! "${listen_port}" =~ ^[0-9]+$ || "${listen_port}" -lt 1 || "${listen_port}" -gt 65535 ]]; then
    echo "invalid listen port: ${listen_port}" >&2
    exit 1
  fi
fi

chain=""
begin_marker="# NOVAPANEL HY2 BEGIN"
end_marker="# NOVAPANEL HY2 END"
if [[ "${action}" != "purge" ]]; then
  chain="NPHY2_$(printf '%s' "${tag}" | sha256sum | awk '{print substr($1, 1, 12)}')"
  begin_marker="# NOVAPANEL HY2 BEGIN ${chain}"
  end_marker="# NOVAPANEL HY2 END ${chain}"
fi

has_cmd() {
  command -v "$1" >/dev/null 2>&1
}

normalize_ports() {
  local raw="${1:-}"
  local part trimmed start end port
  declare -A seen=()

  emit_port() {
    local value="${1:-}"
    if [[ -z "${value}" || "${value}" -lt 1 || "${value}" -gt 65535 ]]; then
      return 0
    fi
    if [[ -n "${seen[${value}]:-}" ]]; then
      return 0
    fi
    seen["${value}"]=1
    printf '%s\n' "${value}"
  }

  IFS=',' read -r -a parts <<< "${raw}"
  for part in "${parts[@]}"; do
    trimmed="$(printf '%s' "${part}" | tr -d '[:space:]')"
    if [[ -z "${trimmed}" ]]; then
      continue
    fi
    if [[ "${trimmed}" =~ ^([0-9]+)-([0-9]+)$ ]]; then
      start="${BASH_REMATCH[1]}"
      end="${BASH_REMATCH[2]}"
      if [[ "${start}" -lt 1 || "${end}" -gt 65535 || "${start}" -gt "${end}" ]]; then
        echo "invalid server_ports range: ${trimmed}" >&2
        return 1
      fi
      for ((port=start; port<=end; port++)); do
        emit_port "${port}"
      done
      continue
    fi
    if [[ "${trimmed}" =~ ^[0-9]+$ ]]; then
      emit_port "${trimmed}"
      continue
    fi
    echo "invalid server_ports token: ${trimmed}" >&2
    return 1
  done
}

render_ufw_block() {
  local ports="${1:-}"
  local family_label="${2:-}"
  local output=""
  local port
  local protocol

  while IFS= read -r port; do
    if [[ -n "${port}" ]]; then
      for protocol in tcp udp; do
        if [[ -n "${output}" ]]; then
          output="${output}"$'\n'
        fi
        output="${output}-A PREROUTING -p ${protocol} --dport ${port} -j REDIRECT --to-ports ${listen_port}"
      done
    fi
  done <<< "${ports}"

  if [[ -z "${output}" ]]; then
    printf ''
    return 0
  fi

  printf '%s\n' "${begin_marker} ${family_label}"
  printf '%s\n' "${output}"
  printf '%s\n' "${end_marker} ${family_label}"
}

strip_ufw_blocks() {
  local file="$1"
  local tmp

  [[ -f "${file}" ]] || return 0

  tmp="$(mktemp)"
  awk -v begin="${begin_marker}" -v end="${end_marker}" '
    BEGIN { skipping = 0 }
    index($0, begin " ") == 1 || $0 == begin {
      skipping = 1
      next
    }
    index($0, end " ") == 1 || $0 == end {
      skipping = 0
      next
    }
    !skipping { print }
  ' "${file}" > "${tmp}"

  if ! cmp -s "${file}" "${tmp}"; then
    mv "${tmp}" "${file}"
  else
    rm -f "${tmp}"
  fi
}

delete_direct_redirects() {
  local bin="$1"
  local redirects="${2:-}"
  local protocol port target_port

  if ! has_cmd "${bin}"; then
    return 0
  fi

  while read -r protocol port target_port; do
    if [[ -n "${protocol}" && -n "${port}" && -n "${target_port}" ]]; then
      while "${bin}" -t nat -C PREROUTING -p "${protocol}" --dport "${port}" -j REDIRECT --to-ports "${target_port}" >/dev/null 2>&1; do
        "${bin}" -t nat -D PREROUTING -p "${protocol}" --dport "${port}" -j REDIRECT --to-ports "${target_port}" || true
      done
    fi
  done <<< "${redirects}"
}

collect_ufw_block_redirects() {
  local file="$1"

  [[ -f "${file}" ]] || return 0

  awk '
    /^# NOVAPANEL HY2 BEGIN/ {
      in_block = 1
      next
    }
    /^# NOVAPANEL HY2 END/ {
      in_block = 0
      next
    }
    in_block && $1 == "-A" && $2 == "PREROUTING" {
      proto = ""
      dport = ""
      target = ""
      for (i = 1; i <= NF; i++) {
        if ($i == "-p" && i + 1 <= NF) proto = $(i + 1)
        if ($i == "--dport" && i + 1 <= NF) dport = $(i + 1)
        if ($i == "--to-ports" && i + 1 <= NF) target = $(i + 1)
      }
      if (proto != "" && dport != "" && target != "") {
        print proto, dport, target
      }
    }
  ' "${file}"
}

remove_ufw_live_redirects() {
  local file="$1"
  local bin="$2"
  local redirects

  redirects="$(collect_ufw_block_redirects "${file}")"
  if [[ -n "${redirects}" ]]; then
    delete_direct_redirects "${bin}" "${redirects}"
  fi
}

remove_port_live_redirects() {
  local bin="$1"
  local normalized_ports="${2:-}"
  local port
  local protocol

  if ! has_cmd "${bin}"; then
    return 0
  fi

  while IFS= read -r port; do
    if [[ -n "${port}" && -n "${listen_port}" && "${listen_port}" =~ ^[0-9]+$ ]]; then
      for protocol in tcp udp; do
        while "${bin}" -t nat -C PREROUTING -p "${protocol}" --dport "${port}" -j REDIRECT --to-ports "${listen_port}" >/dev/null 2>&1; do
          "${bin}" -t nat -D PREROUTING -p "${protocol}" --dport "${port}" -j REDIRECT --to-ports "${listen_port}" || true
        done
      done
    fi
  done <<< "${normalized_ports}"
}

rewrite_ufw_file() {
  local file="$1"
  local block="$2"
  local tmp
  local has_nat=0

  if [[ ! -f "${file}" && -z "${block}" ]]; then
    return 0
  fi

  tmp="$(mktemp)"
  if [[ -f "${file}" ]]; then
    awk -v block="${block}" -v begin="${begin_marker}" -v end="${end_marker}" '
      BEGIN {
        in_nat = 0
        skip = 0
        inserted = 0
        saw_nat = 0
      }
      $0 == begin || $0 == end || index($0, begin " ") == 1 || index($0, end " ") == 1 {
        if ($0 == begin || index($0, begin " ") == 1) {
          skip = 1
        } else if ($0 == end || index($0, end " ") == 1) {
          skip = 0
        }
        next
      }
      skip {
        next
      }
      $0 == "*nat" {
        saw_nat = 1
        in_nat = 1
        print
        next
      }
      in_nat && $0 == "COMMIT" {
        if (block != "") {
          print block
          inserted = 1
        }
        print
        in_nat = 0
        next
      }
      {
        print
      }
      END {
        if (!saw_nat && block != "") {
          print "*nat"
          print ":PREROUTING ACCEPT [0:0]"
          print ":INPUT ACCEPT [0:0]"
          print ":OUTPUT ACCEPT [0:0]"
          print ":POSTROUTING ACCEPT [0:0]"
          print block
          print "COMMIT"
        } else if (saw_nat && block != "" && inserted == 0) {
          print block
        }
      }
    ' "${file}" > "${tmp}"
  else
    if [[ -z "${block}" ]]; then
      rm -f "${tmp}"
      return 0
    fi
    {
      printf '%s\n' '*nat'
      printf '%s\n' ':PREROUTING ACCEPT [0:0]'
      printf '%s\n' ':INPUT ACCEPT [0:0]'
      printf '%s\n' ':OUTPUT ACCEPT [0:0]'
      printf '%s\n' ':POSTROUTING ACCEPT [0:0]'
      printf '%s\n' "${block}"
      printf '%s\n' 'COMMIT'
    } > "${tmp}"
  fi

  if [[ ! -f "${file}" ]] || ! cmp -s "${file}" "${tmp}"; then
    mv "${tmp}" "${file}"
  else
    rm -f "${tmp}"
  fi
}

reload_ufw() {
  if has_cmd ufw && ufw status 2>/dev/null | grep -q '^Status: active'; then
    ufw reload >/dev/null
  fi
}

apply_ufw_allow_rules() {
  local normalized_ports="${1:-}"
  local port

  if ! has_cmd ufw; then
    return 0
  fi

  while IFS= read -r port; do
    if [[ -n "${port}" ]]; then
      ufw allow "${port}" comment "NovaPanel ${chain}" >/dev/null
    fi
  done <<< "${normalized_ports}"
}

remove_ufw_allow_rules() {
  local normalized_ports="${1:-}"
  local port

  if ! has_cmd ufw; then
    return 0
  fi

  while IFS= read -r port; do
    if [[ -n "${port}" ]]; then
      ufw --force delete allow "${port}" >/dev/null 2>&1 || true
    fi
  done <<< "${normalized_ports}"
}

remove_iptables() {
  local bin="$1"
  local normalized_ports="${2:-}"
  local protocol
  if ! has_cmd "${bin}"; then
    return 0
  fi

  remove_port_live_redirects "${bin}" "${normalized_ports}"

  for protocol in tcp udp; do
    while "${bin}" -t nat -C PREROUTING -p "${protocol}" -j "${chain}" >/dev/null 2>&1; do
      "${bin}" -t nat -D PREROUTING -p "${protocol}" -j "${chain}" || true
    done
  done

  "${bin}" -t nat -F "${chain}" 2>/dev/null || true
  "${bin}" -t nat -X "${chain}" 2>/dev/null || true
}

purge_iptables_bin() {
  local bin="$1"
  local chain_name

  if ! has_cmd "${bin}"; then
    return 0
  fi

  while IFS= read -r chain_name; do
    [[ "${chain_name}" == NPHY2_* ]] || continue
    for protocol in tcp udp; do
      while "${bin}" -t nat -C PREROUTING -p "${protocol}" -j "${chain_name}" >/dev/null 2>&1; do
        "${bin}" -t nat -D PREROUTING -p "${protocol}" -j "${chain_name}" || true
      done
    done
    "${bin}" -t nat -F "${chain_name}" 2>/dev/null || true
    "${bin}" -t nat -X "${chain_name}" 2>/dev/null || true
  done < <(
    "${bin}" -t nat -S 2>/dev/null | awk '/^:NPHY2_/ {sub(/^:/, "", $1); print $1} /^-A PREROUTING .* -j NPHY2_/ {print $NF}' | sort -u
  )
}

apply_iptables() {
  local bin="$1"
  local normalized_ports="${2:-}"
  local port
  local protocol

  if ! has_cmd "${bin}"; then
    return 0
  fi

  "${bin}" -t nat -N "${chain}" 2>/dev/null || true
  "${bin}" -t nat -F "${chain}" 2>/dev/null || true

  if [[ -z "${normalized_ports}" ]]; then
    remove_iptables "${bin}"
    return 0
  fi

  for protocol in tcp udp; do
    if ! "${bin}" -t nat -C PREROUTING -p "${protocol}" -j "${chain}" >/dev/null 2>&1; then
      "${bin}" -t nat -A PREROUTING -p "${protocol}" -j "${chain}"
    fi
  done

  while IFS= read -r port; do
    if [[ -n "${port}" ]]; then
      for protocol in tcp udp; do
        "${bin}" -t nat -A "${chain}" -p "${protocol}" --dport "${port}" -j REDIRECT --to-ports "${listen_port}"
      done
    fi
  done <<< "${normalized_ports}"
}

remove_nftables_family() {
  local family="$1"

  if ! has_cmd nft; then
    return 0
  fi

  nft delete chain "${family}" nat "${chain}" 2>/dev/null || true
}

purge_nftables_family() {
  local family="$1"
  local chain_name

  if ! has_cmd nft; then
    return 0
  fi

  while IFS= read -r chain_name; do
    [[ -n "${chain_name}" ]] || continue
    nft delete chain "${family}" nat "${chain_name}" 2>/dev/null || true
  done < <(nft -a list table "${family}" nat 2>/dev/null | awk '/^chain NPHY2_/ {print $2}')
}

apply_nftables_family() {
  local family="$1"
  local normalized_ports="${2:-}"
  local port
  local protocol

  if ! has_cmd nft; then
    return 0
  fi

  nft add table "${family}" nat 2>/dev/null || true
  nft delete chain "${family}" nat "${chain}" 2>/dev/null || true

  if [[ -z "${normalized_ports}" ]]; then
    return 0
  fi

  nft add chain "${family}" nat "${chain}" '{ type nat hook prerouting priority -100; policy accept; }'

  while IFS= read -r port; do
    if [[ -n "${port}" ]]; then
      for protocol in tcp udp; do
        nft add rule "${family}" nat "${chain}" "${protocol}" dport "${port}" redirect to :"${listen_port}"
      done
    fi
  done <<< "${normalized_ports}"
}

apply_ufw_file() {
  local file="$1"
  local family_label="$2"
  local normalized_ports="${3:-}"
  local block

  block="$(render_ufw_block "${normalized_ports}" "${family_label}")"
  rewrite_ufw_file "${file}" "${block}"
}

remove_ufw_file() {
  local file="$1"
  rewrite_ufw_file "${file}" ""
}

apply_ufw() {
  local normalized_ports="${1:-}"

  remove_port_live_redirects iptables "${normalized_ports}"
  remove_port_live_redirects ip6tables "${normalized_ports}"
  apply_ufw_allow_rules "${normalized_ports}"
  apply_ufw_file "/etc/ufw/before.rules" "ip" "${normalized_ports}"
  apply_ufw_file "/etc/ufw/before6.rules" "ip6" "${normalized_ports}"
  reload_ufw
}

remove_ufw() {
  local normalized_ports="${1:-}"

  remove_port_live_redirects iptables "${normalized_ports}"
  remove_port_live_redirects ip6tables "${normalized_ports}"
  remove_ufw_allow_rules "${normalized_ports}"
  remove_ufw_file "/etc/ufw/before.rules"
  remove_ufw_file "/etc/ufw/before6.rules"
  reload_ufw
}

purge_ufw() {
  remove_ufw_live_redirects "/etc/ufw/before.rules" iptables
  remove_ufw_live_redirects "/etc/ufw/before6.rules" ip6tables
  strip_ufw_blocks "/etc/ufw/before.rules"
  strip_ufw_blocks "/etc/ufw/before6.rules"
  reload_ufw
}

select_backend() {
  case "${backend_override}" in
    iptables|nftables|ufw)
      printf '%s' "${backend_override}"
      return 0
      ;;
  esac

  if has_cmd ufw && ufw status 2>/dev/null | grep -q '^Status: active'; then
    printf '%s' "ufw"
    return 0
  fi

  if has_cmd iptables || has_cmd ip6tables; then
    printf '%s' "iptables"
    return 0
  fi

  if has_cmd nft; then
    printf '%s' "nftables"
    return 0
  fi

  printf '%s' "none"
}

if [[ "${action}" == "purge" ]]; then
  purge_ufw
  purge_nftables_family ip
  purge_nftables_family ip6
  purge_iptables_bin iptables
  purge_iptables_bin ip6tables
  exit 0
fi

normalized_ports="$(normalize_ports "${ports_csv}")"
backend="$(select_backend)"

case "${action}" in
  apply)
    case "${backend}" in
      ufw)
        apply_ufw "${normalized_ports}"
        ;;
      nftables)
        apply_nftables_family ip "${normalized_ports}"
        apply_nftables_family ip6 "${normalized_ports}"
        ;;
      iptables)
        apply_iptables iptables "${normalized_ports}"
        apply_iptables ip6tables "${normalized_ports}"
        ;;
      none)
        echo "no supported firewall backend found" >&2
        exit 1
        ;;
    esac
    ;;
  remove)
    remove_ufw "${normalized_ports}"
    remove_nftables_family ip
    remove_nftables_family ip6
    remove_iptables iptables "${normalized_ports}"
    remove_iptables ip6tables "${normalized_ports}"
    ;;
esac
