#!/usr/bin/env bash
set -Eeuo pipefail

readonly EXPECTED_JOB="test-fresh-ufw-linux"
readonly EXPECTED_WORKFLOW="发布 NovaPanel"
readonly SSH_PORT=2222
readonly PANEL_PORT=39095
readonly SUB_PORT=39096
readonly DENY_PORT=39998
readonly DENY_SOURCE="198.51.100.77"
readonly POLICY_RC="/usr/sbin/policy-rc.d"
readonly SSH_DROP_IN="/etc/ssh/sshd_config.d/00-novas-fresh-ufw-acceptance.conf"

die() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

require_github_hosted_release_job() {
    [[ "${NOVAS_FRESH_UFW_OPT_IN:-}" == "1" ]] || die "explicit NOVAS_FRESH_UFW_OPT_IN=1 is required"
    [[ "${NOVAS_GITHUB_ACTIONS:-}" == "true" ]] || die "GitHub Actions identity is missing"
    [[ "${NOVAS_RUNNER_ENVIRONMENT:-}" == "github-hosted" ]] || die "refusing non-hosted runners"
    [[ "${NOVAS_GITHUB_JOB:-}" == "$EXPECTED_JOB" ]] || die "refusing a non-dedicated workflow job"
    [[ "${NOVAS_GITHUB_WORKFLOW:-}" == "$EXPECTED_WORKFLOW" ]] || die "refusing a different workflow"
    [[ "${NOVAS_GITHUB_EVENT_NAME:-}" == "workflow_dispatch" ]] || die "refusing a non-release workflow event"
    [[ "${NOVAS_GITHUB_SERVER_URL:-}" == "https://github.com" ]] || die "refusing a non-GitHub server"
    [[ "${NOVAS_GITHUB_REPOSITORY:-}" == "CatMsg/NovaPanel" ]] || die "refusing a different repository"
    [[ "${NOVAS_GITHUB_RUN_ID:-}" =~ ^[0-9]+$ && "${NOVAS_GITHUB_RUN_ATTEMPT:-}" =~ ^[0-9]+$ ]] || die "GitHub run identity is incomplete"
    [[ "${NOVAS_RUNNER_OS:-}" == "Linux" && "$(uname -s)" == "Linux" ]] || die "this test is Linux-only"
    [[ "${NOVAS_RUNNER_NAME:-}" == "GitHub Actions "* ]] || die "unexpected GitHub runner name"
    [[ "${NOVAS_RUNNER_TEMP:-}" == "/home/runner/work/_temp" ]] || die "unexpected hosted-runner temporary directory"
    case "${NOVAS_WORKSPACE:-}" in
        /home/runner/work/*/*) ;;
        *) die "workspace is not on the GitHub-hosted runner" ;;
    esac
    [[ "$EUID" -eq 0 ]] || die "run this guarded test through sudo"
    [[ "${SUDO_USER:-}" == "runner" ]] || die "the invoking GitHub runner user could not be verified"
    [[ "$(id -u "$SUDO_USER")" == "$(id -u runner)" && "$(id -u runner)" != "0" ]] || die "the invoking user must be the hosted runner account"
}

ufw_user_rules() {
    LC_ALL=C ufw show added | awk '$1 == "ufw"'
}

ssh_refuse() {
    printf 'FAIL: SSH inspection: %s\n' "$1" >&2
    return 1
}

ssh_config_ports() {
    awk '
        $1 == "port" || $1 == "listenaddress" {
            port = $2
            sub(/^.*:/, "", port)
            if (port !~ /^[0-9]+$/ || port < 1 || port > 65535) bad = 1
            else print port + 0
            count++
        }
        END { if (bad || !count) exit 1 }
    ' | sort -nu
}

ssh_unit_state() {
    local unit state
    for unit in ssh.service sshd.service ssh.socket sshd.socket; do
        state=$(LC_ALL=C systemctl show "$unit" \
            -p Id -p LoadState -p ActiveState -p SubState -p MainPID \
            -p UnitFileState -p ExecStart -p Listen -p Triggers -p ControlGroup) || {
            grep -Fxq 'LoadState=not-found' <<<"$state" &&
                grep -Fxq 'ActiveState=inactive' <<<"$state" || return 1
        }
        printf '%s\n%s\n' "$unit" "$state"
    done
}

ssh_config_fingerprint() {
    # Hash locally only; never print configuration or host keys to CI logs.
    find /etc/ssh -type f ! -path "$SSH_DROP_IN" -print0 | sort -z | xargs -0 -r sha256sum || return 1
    if [[ -f /etc/default/ssh ]]; then sha256sum /etc/default/ssh || return 1; fi
    sha256sum "$SSHD_BIN"
}

process_identity() {
    local pid="$1" stat
    [[ "$pid" =~ ^[0-9]+$ ]] || return 1
    [[ "$(readlink -f "/proc/$pid/exe")" == "$(readlink -f "$SSHD_BIN")" ]] || return 1
    awk '$1 == "Uid:" { if ($2 != 0 || $3 != 0 || $4 != 0 || $5 != 0) exit 1; found = 1 }
        END { if (!found) exit 1 }' "/proc/$pid/status" || return 1
    stat=$(<"/proc/$pid/stat") || return 1
    stat="${stat##*) }"
    # starttime distinguishes the owned child from a reused PID during cleanup.
    awk '{ print $20 }' <<<"$stat"
}

baseline_sshd_command_valid() {
    case "$1" in
        "$SSHD_BIN -D" | "$SSHD_BIN -D -e") ;;
        *)
            # OpenSSH rewrites argv into its listener title. Reject -f/-p/-o
            # overrides rather than guessing the running daemon's config.
            [[ "$1" =~ ^sshd:\ /usr/sbin/sshd\ -D(\ -e)?\ \[listener\]\ [0-9]+\ of\ [0-9]+-[0-9]+\ startups$ ]] || return 1
            ;;
    esac
}

verify_baseline_sshd_pid() {
    local pid="$1" unit main_pid args
    process_identity "$pid" >/dev/null || return 1
    args=$(tr '\0' ' ' <"/proc/$pid/cmdline" | sed 's/[[:space:]]*$//') || return 1
    baseline_sshd_command_valid "$args" || return 1
    for unit in ssh.service sshd.service; do
        systemctl is-active --quiet "$unit" || continue
        main_pid=$(systemctl show "$unit" -p MainPID --value) || return 1
        [[ "$main_pid" == "$pid" ]] && return 0
    done
    return 1
}

verify_baseline_socket() {
    local endpoint="$1" port="${1##*:}" unit triggers listeners binding kind extra matched=0
    for unit in ssh.socket sshd.socket; do
        systemctl is-active --quiet "$unit" || continue
        triggers=$(systemctl show "$unit" -p Triggers --value) || return 1
        case "$triggers" in ssh.service | sshd.service) ;; *) return 1 ;; esac
        listeners=$(systemctl show "$unit" -p Listen --value) || return 1
        [[ -n "$listeners" ]] || return 1
        # Ubuntu's socket units may use bare ports, [::]:port, or explicit IPs.
        while read -r binding kind extra; do
            [[ "$kind" == '(Stream)' && -z "$extra" ]] || return 1
            case "$binding" in
                "$endpoint") matched=1 ;;
                "$port" | "[::]:$port")
                    [[ "$endpoint" == "*:$port" || "$endpoint" == "[::]:$port" ]] && matched=1
                    ;;
                "0.0.0.0:$port") [[ "$endpoint" == "0.0.0.0:$port" ]] && matched=1 ;;
            esac
        done < <(sed 's/) /)\n/g' <<<"$listeners")
    done
    [[ "$matched" == 1 ]]
}

inspect_baseline_ssh() {
    local listeners="$1" configured="$2" expected_ports="$3" line endpoint port owners owner name pid fd remainder normalized_owners
    local found_ports="" baseline="" configured_ports
    configured_ports=$(ssh_config_ports <<<"$configured") || return 1
    while IFS= read -r line; do
        [[ -n "$line" ]] || continue
        endpoint=$(awk '{print $4}' <<<"$line")
        port="${endpoint##*:}"
        case "$port" in "$SSH_PORT" | "$PANEL_PORT" | "$SUB_PORT" | "$DENY_PORT") ssh_refuse "reserved test port $port is occupied"; return 1 ;; esac
        # Inspect both SSH-named processes and every configured SSH endpoint.
        if [[ "$line" != *'"sshd"'* && "$line" != *'"sshd-session"'* ]] &&
            ! grep -Fxq "$port" <<<"$configured_ports" && ! grep -Fxq "$port" <<<"$expected_ports"; then
            continue
        fi
        grep -Fxq "$port" <<<"$expected_ports" || { ssh_refuse "listener port differs from validated baseline"; return 1; }
        [[ "$line" == *'users:('* ]] || { ssh_refuse "listener process ownership is unavailable"; return 1; }
        owners="${line#*users:}"
        remainder="$owners"
        normalized_owners=""
        while [[ "$remainder" =~ \"([^\"]+)\",pid=([0-9]+),fd=([0-9]+) ]]; do
            owner="${BASH_REMATCH[0]}" name="${BASH_REMATCH[1]}" pid="${BASH_REMATCH[2]}" fd="${BASH_REMATCH[3]}"
            case "$name" in
                sshd) verify_baseline_sshd_pid "$pid" || { ssh_refuse "sshd is not a root system-service MainPID using the default config (overrides refused)"; return 1; } ;;
                systemd) [[ "$pid" == 1 ]] && verify_baseline_socket "$endpoint" || { ssh_refuse "socket listener lacks an exact active SSH unit binding"; return 1; } ;;
                *) ssh_refuse "unsupported or mixed SSH listener owner"; return 1 ;;
            esac
            normalized_owners+="$name,pid=$pid,fd=$fd"$'\n'
            remainder="${remainder/"$owner"/}"
        done
        [[ "${remainder//[(),[:space:]]/}" == "" && "$remainder" != "$owners" ]] || { ssh_refuse "listener ownership syntax is ambiguous"; return 1; }
        # Queue/backlog counters and ss owner ordering are not ownership state.
        owners=$(printf '%s' "$normalized_owners" | sort | tr '\n' ' ')
        baseline+="$endpoint $owners"$'\n'
        found_ports+="$port"$'\n'
    done <<<"$listeners"
    found_ports=$(sed '/^$/d' <<<"$found_ports" | sort -nu)
    [[ -n "$found_ports" && "$found_ports" == "$expected_ports" ]] || { ssh_refuse "actual SSH port set differs from validated baseline"; return 1; }
    printf '%s' "$baseline" | sort
}

verify_ssh_baseline_unchanged() {
    local listeners effective baseline
    listeners=$(LC_ALL=C ss -H -ltnp) || return 1
    effective=$(LC_ALL=C "$SSHD_BIN" -T) || return 1
    [[ "$effective" == "$INITIAL_SSH_EFFECTIVE" ]] || { ssh_refuse "host effective config changed"; return 1; }
    [[ "$(installer_ssh_ports)" == "$INITIAL_SSH_PORTS" ]] || { ssh_refuse "production-discovered baseline ports changed"; return 1; }
    baseline=$(inspect_baseline_ssh "$listeners" "$effective" "$INITIAL_SSH_PORTS") || return 1
    [[ "$baseline" == "$INITIAL_SSH_LISTENERS" ]] || { ssh_refuse "host listener endpoints or PID/fd ownership changed"; return 1; }
    [[ "$(ssh_unit_state)" == "$INITIAL_SSH_UNITS" ]] || { ssh_refuse "host service/socket state changed"; return 1; }
    [[ "$(ssh_config_fingerprint)" == "$INITIAL_SSH_FINGERPRINT" ]] || { ssh_refuse "host SSH files or executable changed"; return 1; }
}

owned_ssh_listeners_match() {
    awk -v pid="$SSHD_PID" -v port="$SSH_PORT" '
        $4 ~ (":" port "$") {
            if ($4 == "127.0.0.1:" port) v4++
            else if ($4 == "[::1]:" port) v6++
            else bad = 1
            ownership = substr($0, index($0, "users:"))
            expected = "^users:\\(\\(\"sshd\",pid=" pid ",fd=[0-9]+\\)\\)$"
            if (ownership !~ expected) bad = 1
            count++
        }
        END { exit (bad || count != 2 || v4 != 1 || v6 != 1) }
    '
}

verify_owned_ssh_listeners() {
    local listeners baseline
    [[ "$(process_identity "$SSHD_PID")" == "$SSHD_IDENTITY" ]] || return 1
    listeners=$(LC_ALL=C ss -H -ltnp) || return 1
    owned_ssh_listeners_match <<<"$listeners" || return 1
    # Remove only our reserved listener rows before rechecking the host baseline.
    listeners=$(awk -v port="$SSH_PORT" '$4 !~ (":" port "$")' <<<"$listeners")
    baseline=$(inspect_baseline_ssh "$listeners" "$INITIAL_SSH_EFFECTIVE" "$INITIAL_SSH_PORTS") || return 1
    [[ "$baseline" == "$INITIAL_SSH_LISTENERS" ]] || return 1
    [[ "$(ssh_unit_state)" == "$INITIAL_SSH_UNITS" ]] || return 1
    [[ "$(ssh_config_fingerprint)" == "$INITIAL_SSH_FINGERPRINT" ]] || return 1
}

read_runner_crontab() {
    local destination="$1" error_file="${1}.stderr" status
    if crontab -u "$RUNNER_USER" -l >"$destination" 2>"$error_file"; then
        CRON_READ_PRESENT=1
    else
        status=$?
        if [[ "$status" -eq 1 ]] && grep -Fxq "no crontab for $RUNNER_USER" "$error_file"; then
            : >"$destination"
            CRON_READ_PRESENT=0
        else
            cat "$error_file" >&2
            rm -f "$error_file"
            return 1
        fi
    fi
    rm -f "$error_file"
}

filter_owned_acme_jobs() {
    local mode="$1" source_file="$2"
    awk -v mode="$mode" -v acme_home="$ACME_USER_HOME/.acme.sh" '
        function has_exact_home_argument(tail, quoted_home, plain_home, after) {
            sub(/^[[:space:]]+/, "", tail)
            if (substr(tail, 1, 6) != "--home") return 0
            tail = substr(tail, 7)
            if (tail !~ /^[[:space:]]/) return 0
            sub(/^[[:space:]]+/, "", tail)
            if (substr(tail, 1, length(quoted_home)) == quoted_home) {
                after = substr(tail, length(quoted_home) + 1, 1)
                return after == "" || after ~ /[[:space:]]/
            }
            if (substr(tail, 1, length(plain_home)) == plain_home) {
                after = substr(tail, length(plain_home) + 1, 1)
                return after == "" || after ~ /[[:space:]]/
            }
            return 0
        }
        function is_owned_job(line, command, quoted_script, plain_script, quoted_home, plain_home, tail, field) {
            if (line ~ /^[[:space:]]*#/) return 0
            command = line
            sub(/^[[:space:]]+/, "", command)
            for (field = 1; field <= 5; field++) {
                if (command !~ /^[^[:space:]]+[[:space:]]+/) return 0
                sub(/^[^[:space:]]+[[:space:]]+/, "", command)
            }
            quoted_script = "\"" acme_home "\"/acme.sh --cron"
            plain_script = acme_home "/acme.sh --cron"
            quoted_home = "\"" acme_home "\""
            plain_home = acme_home
            if (substr(command, 1, length(quoted_script)) == quoted_script) {
                tail = substr(command, length(quoted_script) + 1)
                return has_exact_home_argument(tail, quoted_home, plain_home)
            }
            if (substr(command, 1, length(plain_script)) == plain_script) {
                tail = substr(command, length(plain_script) + 1)
                return has_exact_home_argument(tail, quoted_home, plain_home)
            }
            return 0
        }
        is_owned_job($0) {
            count++
            next
        }
        mode == "strip" { print }
        END { if (mode == "count") print count + 0 }
    ' "$source_file"
}

strip_owned_acme_jobs() {
    filter_owned_acme_jobs strip "$1" >"$2"
}

count_owned_acme_jobs() {
    filter_owned_acme_jobs count "$1"
}

cleanup() {
    local original_status=$? status current_rules port rule
    local cleanup_failed=0 firewall_inactive=0
    trap - EXIT INT TERM
    set +e

    if [[ -n "${SSHD_PID:-}" ]] && kill -0 "$SSHD_PID" 2>/dev/null; then
        if [[ -n "${SSHD_IDENTITY:-}" && "$(process_identity "$SSHD_PID")" == "$SSHD_IDENTITY" ]]; then
            kill "$SSHD_PID" 2>/dev/null || cleanup_failed=1
            wait "$SSHD_PID" 2>/dev/null || true
        else
            printf 'FAIL: temporary sshd PID ownership changed; refusing to signal it\n' >&2
            cleanup_failed=1
        fi
    fi

    if command -v ufw >/dev/null 2>&1; then
        if status=$(LC_ALL=C ufw status 2>/dev/null); then
            if [[ "${UFW_ACTIVATION_OWNED:-0}" == "1" && "$status" == Status:\ active* ]]; then
                LC_ALL=C ufw --force disable >/dev/null || cleanup_failed=1
                status=$(LC_ALL=C ufw status 2>/dev/null) || cleanup_failed=1
            fi
            [[ "$status" == Status:\ inactive* ]] && firewall_inactive=1
        fi
        if [[ "$firewall_inactive" != 1 ]]; then
            printf 'FAIL: cleanup cannot confirm inactive UFW; retaining all safety rules\n' >&2
            cleanup_failed=1
        fi
        if [[ "$firewall_inactive" == 1 && "$ALLOW_RULES_OWNED" == "1" ]]; then
            for port in "${OWNED_ALLOW_PORTS[@]}"; do
                rule="ufw allow $port/tcp"
                while ufw_user_rules 2>/dev/null | grep -Fxq "$rule"; do
                    LC_ALL=C ufw --force delete allow "$port/tcp" >/dev/null || { cleanup_failed=1; break; }
                done
            done
        fi
        if [[ "$firewall_inactive" == 1 && "$DENY_RULE_OWNED" == "1" ]]; then
            rule="ufw deny from $DENY_SOURCE to any port $DENY_PORT proto tcp"
            while ufw_user_rules 2>/dev/null | grep -Fxq "$rule"; do
                LC_ALL=C ufw --force delete deny from "$DENY_SOURCE" to any port "$DENY_PORT" proto tcp >/dev/null || { cleanup_failed=1; break; }
            done
        fi
        if [[ "${UFW_BASELINE_READY:-0}" == "1" ]]; then
            current_rules="${WORK_DIR}/ufw-rules-after-cleanup"
            ufw_user_rules >"$current_rules" || cleanup_failed=1
            cmp -s "${WORK_DIR}/ufw-rules-before-test" "$current_rules" || cleanup_failed=1
        fi
    fi

    if [[ "${UFW_CONFIG_BACKUP_READY:-0}" == "1" && "$firewall_inactive" == 1 ]]; then
        cp -p "${WORK_DIR}/ufw-default-before-test" /etc/default/ufw || cleanup_failed=1
        cp -p "${WORK_DIR}/ufw-conf-before-test" /etc/ufw/ufw.conf || cleanup_failed=1
        if [[ "$UFW_DEFAULT_BAK_PRESENT" == 1 ]]; then
            cp -p "$WORK_DIR/ufw-default-bak-before-test" /etc/default/ufw.bak || cleanup_failed=1
        else
            rm -f /etc/default/ufw.bak || cleanup_failed=1
        fi
    elif [[ "${UFW_CONFIG_BACKUP_READY:-0}" == "1" ]]; then
        printf 'FAIL: retaining active UFW configuration and recovery snapshots\n' >&2
        cleanup_failed=1
    fi

    if [[ "${ACME_CRONTAB_READY:-0}" == "1" ]]; then
        local current_cron="${WORK_DIR}/runner-crontab-current"
        local filtered_cron="${WORK_DIR}/runner-crontab-clean"
        local verified_cron="${WORK_DIR}/runner-crontab-verified"
        if read_runner_crontab "$current_cron"; then
            local current_present="$CRON_READ_PRESENT"
            strip_owned_acme_jobs "$current_cron" "$filtered_cron" || cleanup_failed=1
            if [[ "$ACME_CRONTAB_WAS_PRESENT" == "0" && ! -s "$filtered_cron" ]]; then
                if [[ "$current_present" == "1" ]]; then
                    crontab -u "$RUNNER_USER" -r || cleanup_failed=1
                fi
            elif [[ "$current_present" == "1" || "$ACME_CRONTAB_WAS_PRESENT" == "1" ]]; then
                crontab -u "$RUNNER_USER" "$filtered_cron" || cleanup_failed=1
            fi
            if read_runner_crontab "$verified_cron"; then
                local verified_present="$CRON_READ_PRESENT"
                cmp -s "$ACME_CRONTAB_BEFORE" "$verified_cron" || cleanup_failed=1
                [[ "$verified_present" == "$ACME_CRONTAB_WAS_PRESENT" ]] || cleanup_failed=1
            else
                cleanup_failed=1
            fi
        else
            cleanup_failed=1
        fi
    fi

    if [[ "${POLICY_RC_CREATED:-0}" == "1" ]]; then
        if [[ ! -L "$POLICY_RC" && "$(stat -Lc '%d:%i' "$POLICY_RC")" == "$POLICY_RC_IDENTITY" ]] &&
            cmp -s "$WORK_DIR/policy-rc.d" "$POLICY_RC"; then
            rm -f "$POLICY_RC" || cleanup_failed=1
        else
            cleanup_failed=1
        fi
    fi
    if [[ "${SSHD_CONFIG_CREATED:-0}" == "1" ]]; then
        if [[ "$(stat -Lc '%d:%i' "$SSH_DROP_IN")" == "$SSH_DROP_IN_IDENTITY" ]] &&
            [[ ! -L "$SSH_DROP_IN" ]] && cmp -s "$WORK_DIR/ssh-global-drop-in" "$SSH_DROP_IN"; then
            rm -f "$SSH_DROP_IN" || cleanup_failed=1
        else
            printf 'FAIL: temporary SSH drop-in ownership changed; refusing removal\n' >&2
            cleanup_failed=1
        fi
    fi
    if [[ "${SSH_BASELINE_READY:-0}" == "1" ]]; then
        verify_ssh_baseline_unchanged || cleanup_failed=1
    fi
    if [[ "${SSHD_RUN_DIR_CREATED:-0}" == "1" ]]; then
        rmdir /run/sshd || cleanup_failed=1
    fi
    if [[ "$cleanup_failed" == 0 && -n "${WORK_DIR:-}" && -d "$WORK_DIR" ]]; then
        rm -rf "$WORK_DIR" || cleanup_failed=1
    fi

    if [[ "$cleanup_failed" -ne 0 && "$original_status" -eq 0 ]]; then
        original_status=1
    fi
    if [[ "$cleanup_failed" -ne 0 ]]; then
        printf 'FAIL: acceptance cleanup was incomplete; inspect this disposable runner before reuse\n' >&2
    fi
    exit "$original_status"
}

require_github_hosted_release_job

[[ $# -eq 1 ]] || die "usage: $0 /home/runner/work/_temp/novas-fresh-ufw-test"
TEST_BINARY="$1"
[[ "$TEST_BINARY" == "$NOVAS_RUNNER_TEMP/"* && -x "$TEST_BINARY" && ! -L "$TEST_BINARY" ]] || die "test-built novas binary must be executable and inside RUNNER_TEMP"
[[ -f /etc/os-release ]] || die "Ubuntu release identity is unavailable"
# shellcheck disable=SC1091
source /etc/os-release
[[ "${ID:-}" == "ubuntu" ]] || die "refusing to install packages outside Ubuntu"
command -v ss >/dev/null 2>&1 || die "ss must already exist for the listener preflight"
command -v systemctl >/dev/null 2>&1 || die "systemctl is required for the native cron acceptance"
SSHD_BIN=$(command -v sshd) || die "OpenSSH daemon executable is unavailable"
[[ "$SSHD_BIN" == /* && -x "$SSHD_BIN" ]] || die "resolved sshd path must be absolute and executable"
source "${NOVAS_WORKSPACE}/install.sh"
INITIAL_SSH_EFFECTIVE=$(LC_ALL=C "$SSHD_BIN" -T) || die "cannot inspect the host effective SSH configuration"
INITIAL_SSH_PORTS=$(installer_ssh_ports) || die "host SSH configuration/listeners failed the production read-only precheck"
initial_listeners=$(LC_ALL=C ss -H -ltnp) || die "cannot inspect listeners before package installation"
INITIAL_SSH_LISTENERS=$(inspect_baseline_ssh "$initial_listeners" "$INITIAL_SSH_EFFECTIVE" "$INITIAL_SSH_PORTS") || die "unknown SSH ownership or reserved-port listener; refusing package installation"
INITIAL_SSH_UNITS=$(ssh_unit_state) || die "cannot snapshot host SSH units"
INITIAL_SSH_FINGERPRINT=$(ssh_config_fingerprint) || die "cannot snapshot host SSH configuration"
[[ -d /etc/ssh/sshd_config.d ]] || die "OpenSSH drop-in directory is unavailable"
grep -Eq '^[[:space:]]*Include[[:space:]]+/etc/ssh/sshd_config\.d/\*' /etc/ssh/sshd_config || die "default sshd configuration does not include drop-ins"
[[ ! -e "$SSH_DROP_IN" && ! -L "$SSH_DROP_IN" ]] || die "acceptance sshd drop-in already exists"
printf 'INFO: read-only host SSH baseline verified; ports=%s; service/socket ownership and config fingerprint captured\n' "$(tr '\n' ',' <<<"$INITIAL_SSH_PORTS" | sed 's/,$//')"

if command -v ufw >/dev/null 2>&1; then
    initial_ufw_status=$(LC_ALL=C ufw status) || die "cannot read existing UFW state"
    case "$initial_ufw_status" in
        'Status: inactive'*) ;;
        'Status: active'*) die "refusing to run with pre-existing active UFW" ;;
        *) die "existing UFW state is ambiguous; refusing package installation" ;;
    esac
    INITIAL_UFW_RULES=$(ufw_user_rules) || die "cannot snapshot original UFW rules"
fi
if [[ -f /etc/ufw/ufw.conf ]] && grep -Eq '^[[:space:]]*ENABLED[[:space:]]*=[[:space:]]*yes([[:space:]]|$)' /etc/ufw/ufw.conf; then
    die "existing UFW configuration is enabled; refusing to continue"
fi
if [[ -e "$POLICY_RC" || -L "$POLICY_RC" ]]; then
    die "pre-existing policy-rc.d prevents safe package-service isolation"
fi

WORK_DIR=$(mktemp -d "$NOVAS_RUNNER_TEMP/novas-fresh-ufw.XXXXXX") || die "cannot create a runner-local temporary directory"
SSHD_PID=""
SSHD_RUN_DIR_CREATED=0
POLICY_RC_CREATED=0
SSHD_CONFIG_CREATED=0
DENY_RULE_OWNED=0
ALLOW_RULES_OWNED=0
OWNED_ALLOW_PORTS=()
UFW_ACTIVATION_OWNED=0
SSH_BASELINE_READY=1
UFW_BASELINE_READY=0
UFW_CONFIG_BACKUP_READY=0
ACME_CRONTAB_READY=0
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

chmod 0755 "$WORK_DIR"
printf '#!/bin/sh\nexit 101\n' >"$WORK_DIR/policy-rc.d"
(set -o noclobber; cat "$WORK_DIR/policy-rc.d" >"$POLICY_RC") || die "cannot exclusively create package-service guard"
POLICY_RC_IDENTITY=$(stat -Lc '%d:%i' "$POLICY_RC")
POLICY_RC_CREATED=1
chmod 0755 "$POLICY_RC"
export DEBIAN_FRONTEND=noninteractive
export NEEDRESTART_MODE=l
apt-get update || die "guarded package index update failed (SSH baseline retained)"
# Do not upgrade the already inspected host OpenSSH packages/configuration.
apt-get install -y --no-upgrade --no-install-recommends ufw openssh-client iproute2 cron || die "guarded no-upgrade package installation failed"
[[ ! -L "$POLICY_RC" && "$(stat -Lc '%d:%i' "$POLICY_RC")" == "$POLICY_RC_IDENTITY" ]] &&
    cmp -s "$WORK_DIR/policy-rc.d" "$POLICY_RC" || die "package-service guard ownership changed"
rm -f "$POLICY_RC"
POLICY_RC_CREATED=0
command -v crontab >/dev/null 2>&1 || die "the guarded cron package installation did not provide crontab"

verify_ssh_baseline_unchanged || die "package installation changed host SSH configuration, ownership or listeners"
printf 'INFO: package stage complete; host SSH baseline unchanged; no daemon reload requested\n'
[[ "$(LC_ALL=C ufw status | sed -n '1p')" == "Status: inactive" ]] || die "UFW is not inactive after package installation"
if [[ ! -d /run/sshd ]]; then
    mkdir -p /run/sshd
    SSHD_RUN_DIR_CREATED=1
fi

ufw_user_rules >"$WORK_DIR/ufw-rules-before-test" || die "cannot read the initial UFW user rules"
if [[ "${INITIAL_UFW_RULES+x}" == x ]]; then
    [[ "$(<"$WORK_DIR/ufw-rules-before-test")" == "$INITIAL_UFW_RULES" ]] || die "package installation changed original UFW rules"
fi
UFW_BASELINE_READY=1
for reserved_port in "$SSH_PORT" "$PANEL_PORT" "$SUB_PORT"; do
    if grep -Fq "$reserved_port/tcp" "$WORK_DIR/ufw-rules-before-test"; then
        die "initial UFW rules already use reserved test port $reserved_port"
    fi
done
if grep -Fq "from $DENY_SOURCE to any port $DENY_PORT proto tcp" "$WORK_DIR/ufw-rules-before-test"; then
    die "initial UFW rules collide with the acceptance deny-rule fixture"
fi
if [[ -f /etc/default/ufw && ! -L /etc/default/ufw && -f /etc/ufw/ufw.conf && ! -L /etc/ufw/ufw.conf && ! -L /etc/default/ufw.bak ]]; then
    cp -p /etc/default/ufw "$WORK_DIR/ufw-default-before-test"
    cp -p /etc/ufw/ufw.conf "$WORK_DIR/ufw-conf-before-test"
    UFW_DEFAULT_BAK_PRESENT=0
    if [[ -e /etc/default/ufw.bak ]]; then
        [[ -f /etc/default/ufw.bak ]] || die "UFW default backup is not a regular file"
        cp -p /etc/default/ufw.bak "$WORK_DIR/ufw-default-bak-before-test"
        UFW_DEFAULT_BAK_PRESENT=1
    fi
    UFW_CONFIG_BACKUP_READY=1
else
    die "UFW default configuration is missing"
fi

ssh-keygen -q -t ed25519 -N '' -f "$WORK_DIR/ssh_host_ed25519_key"
cat >"$WORK_DIR/sshd_config" <<EOF
Port $SSH_PORT
AddressFamily any
ListenAddress 127.0.0.1
ListenAddress ::1
HostKey $WORK_DIR/ssh_host_ed25519_key
PidFile $WORK_DIR/sshd.pid
PermitRootLogin no
PasswordAuthentication no
KbdInteractiveAuthentication no
PubkeyAuthentication no
UsePAM no
EOF
sshd_effective=$(LC_ALL=C "$SSHD_BIN" -T -f "$WORK_DIR/sshd_config") || die "sshd rejected the temporary listener configuration"
port_count=$(awk '$1 == "port" { count++ } END { print count + 0 }' <<<"$sshd_effective")
[[ "$port_count" == "1" ]] || die "sshd effective configuration contains unexpected ports"
awk -v port="$SSH_PORT" '
    $1 == "port" && $2 == port { found_port = 1 }
    $1 == "listenaddress" {
        address = $2
        if (address == "127.0.0.1:" port) v4++
        else if (address == "[::1]:" port) v6++
        else bad = 1
    }
    END { exit !(found_port && !bad && v4 == 1 && v6 == 1) }
' <<<"$sshd_effective" || die "sshd is not restricted to IPv4/IPv6 loopback on port $SSH_PORT"
"$SSHD_BIN" -t -f "$WORK_DIR/sshd_config"
"$SSHD_BIN" -D -f "$WORK_DIR/sshd_config" -E "$WORK_DIR/sshd.log" &
SSHD_PID=$!
SSHD_IDENTITY=""
for attempt in {1..50}; do
    kill -0 "$SSHD_PID" 2>/dev/null || { cat "$WORK_DIR/sshd.log" >&2; die "temporary sshd exited before listening"; }
    if [[ -z "$SSHD_IDENTITY" ]]; then
        SSHD_IDENTITY=$(process_identity "$SSHD_PID") || SSHD_IDENTITY=""
    fi
    if [[ -n "$SSHD_IDENTITY" ]] && verify_owned_ssh_listeners; then
        break
    fi
    sleep 0.1
done
verify_owned_ssh_listeners || { cat "$WORK_DIR/sshd.log" >&2; die "owned sshd did not bind both loopback families or host SSH changed"; }

# Port is additive, but declaring any Port removes OpenSSH's implicit default.
# Retain the entire inspected baseline before adding our separate daemon's port.
{ ssh_config_ports <<<"$INITIAL_SSH_EFFECTIVE"; printf '%s\n' "$SSH_PORT"; } |
    sort -nu | sed 's/^/Port /' >"$WORK_DIR/ssh-global-drop-in"
(set -o noclobber; cat "$WORK_DIR/ssh-global-drop-in" >"$SSH_DROP_IN") || die "cannot exclusively create temporary SSH port declaration"
SSH_DROP_IN_IDENTITY=$(stat -Lc '%d:%i' "$SSH_DROP_IN")
SSHD_CONFIG_CREATED=1
global_effective=$(LC_ALL=C "$SSHD_BIN" -T) || die "host sshd rejected augmented port declarations"
expected_config_ports=$( { ssh_config_ports <<<"$INITIAL_SSH_EFFECTIVE"; printf '%s\n' "$SSH_PORT"; } | sort -nu)
[[ "$(ssh_config_ports <<<"$global_effective")" == "$expected_config_ports" ]] || die "temporary drop-in did not preserve baseline SSH ports and add $SSH_PORT"
[[ "$(awk '$1 != "port" && $1 != "listenaddress"' <<<"$global_effective")" == \
   "$(awk '$1 != "port" && $1 != "listenaddress"' <<<"$INITIAL_SSH_EFFECTIVE")" ]] || die "temporary port declaration changed non-port SSH configuration"
EXPECTED_SSH_PORTS=$( { printf '%s\n' "$INITIAL_SSH_PORTS" "$SSH_PORT"; } | sort -nu)
[[ "$(installer_ssh_ports)" == "$EXPECTED_SSH_PORTS" ]] || die "production helper did not discover all baseline and owned SSH listeners"
verify_owned_ssh_listeners || die "host SSH changed while declaring test ports"
printf 'INFO: independent loopback sshd verified on IPv4/IPv6 port %s; production helper sees baseline plus test port\n' "$SSH_PORT"

mkdir -p "$WORK_DIR/panel-home"
cp "$TEST_BINARY" "$WORK_DIR/panel-home/novas"
chmod 0755 "$WORK_DIR/panel-home/novas"
NOVAS_DB_FOLDER="$WORK_DIR/panel-home/db" "$WORK_DIR/panel-home/novas" setting -show
[[ -s "$WORK_DIR/panel-home/db/novas.db" ]] || die "the test-built novas CLI did not create its temporary database"

release=ubuntu
DENY_RULE_OWNED=1
ufw --force deny from "$DENY_SOURCE" to any port "$DENY_PORT" proto tcp
grep -Fxq "ufw deny from $DENY_SOURCE to any port $DENY_PORT proto tcp" <(ufw_user_rules) || die "could not establish the unrelated deny-rule fixture"

readarray -t EXPECTED_ALLOW_PORTS < <(printf '%s\n' "$EXPECTED_SSH_PORTS" "$PANEL_PORT" "$SUB_PORT" | sort -nu)
for port in "${EXPECTED_ALLOW_PORTS[@]}"; do
    if ! grep -Fxq "ufw allow $port/tcp" "$WORK_DIR/ufw-rules-before-test"; then
        OWNED_ALLOW_PORTS+=("$port")
    fi
done
ALLOW_RULES_OWNED=1
[[ "$(LC_ALL=C ufw status | sed -n '1p')" == "Status: inactive" ]] || die "UFW activation state changed before installer hook"
UFW_ACTIVATION_OWNED=1
printf 'INFO: invoking native install_fresh_ufw; all actual SSH plus panel/sub must receive dualstack allow rules\n'
install_fresh_ufw "$WORK_DIR/panel-home" "" "$PANEL_PORT" "$SUB_PORT"
verify_owned_ssh_listeners || die "host or owned SSH listeners changed during UFW activation"
[[ "$(installer_ssh_ports)" == "$EXPECTED_SSH_PORTS" ]] || die "SSH port discovery changed during UFW activation"

ufw_status=$(LC_ALL=C ufw status) || die "cannot read UFW status after the installer hook"
[[ "$ufw_status" == *$'Status: active'* ]] || die "UFW is not active after the installer hook"
grep -Eq '^IPV6=yes$' /etc/default/ufw || die "UFW IPv6 is not enabled"
for port in "${EXPECTED_ALLOW_PORTS[@]}"; do
    awk -v rule="$port/tcp" '
        $1 == rule && $2 == "ALLOW" && $3 == "Anywhere" { ipv4 = 1 }
        $1 == rule && $2 == "(v6)" && $3 == "ALLOW" && $4 == "Anywhere" && $5 == "(v6)" { ipv6 = 1 }
        END { exit !(ipv4 && ipv6) }
    ' <<<"$ufw_status" || die "UFW is missing IPv4/IPv6 allow rules for $port/tcp"
done
grep -Fxq "ufw deny from $DENY_SOURCE to any port $DENY_PORT proto tcp" <(ufw_user_rules) || die "the unrelated deny rule was not preserved"
awk -v rule="$DENY_PORT/tcp" -v source="$DENY_SOURCE" '
    $1 == rule && $2 == "DENY" {
        for (field = 3; field <= NF; field++) if ($field == source) found = 1
    }
    END { exit !found }
' <<<"$ufw_status" || die "the unrelated deny rule is missing from active UFW status"
while IFS= read -r baseline_rule; do
    [[ -z "$baseline_rule" ]] || grep -Fxq "$baseline_rule" <(ufw_user_rules) || die "an initial UFW rule was not preserved"
done <"$WORK_DIR/ufw-rules-before-test"

for address in 127.0.0.1 ::1; do
    keyscan_file="$WORK_DIR/ssh-keyscan-${address//:/v6}"
    timeout 10 ssh-keyscan -T 5 -p "$SSH_PORT" "$address" >"$keyscan_file" 2>"$WORK_DIR/ssh-keyscan.stderr" || die "SSH listener reachability check failed while UFW was active at $address:$SSH_PORT"
    [[ -s "$keyscan_file" ]] || die "SSH did not return a host key while UFW was active at $address:$SSH_PORT"
done
printf 'PASS: real sshd remained reachable over IPv4/IPv6 loopback with UFW active; allow rules were independently verified in UFW status\n'

RUNNER_USER="$SUDO_USER"
command -v runuser >/dev/null 2>&1 || die "runuser is required to isolate the ACME cron acceptance to the GitHub runner user"
command -v crontab >/dev/null 2>&1 || die "crontab is unavailable"
systemctl enable --now cron
ACME_USER_HOME="$WORK_DIR/acme-home"
ACME_TMP="$WORK_DIR/acme-tmp"
ACME_FUNCTIONS="$WORK_DIR/acme-functions.sh"
ACME_SYSTEMCTL_BIN="$WORK_DIR/acme-bin"
mkdir -p "$ACME_USER_HOME" "$ACME_TMP" "$ACME_SYSTEMCTL_BIN"
chown -R "$RUNNER_USER":"$(id -gn "$RUNNER_USER")" "$ACME_USER_HOME" "$ACME_TMP"
chmod 0700 "$ACME_USER_HOME" "$ACME_TMP"
chmod 0755 "$ACME_SYSTEMCTL_BIN"
cat >"$ACME_SYSTEMCTL_BIN/systemctl" <<'EOF'
#!/bin/sh
exec /usr/bin/sudo -n /usr/bin/systemctl "$@"
EOF
chmod 0755 "$ACME_SYSTEMCTL_BIN/systemctl"

extract_novas_function() {
    awk -v function_name="$1" '
        $0 == function_name "() {" { in_function = 1 }
        in_function { print }
        in_function && $0 == "}" { exit }
    ' "${NOVAS_WORKSPACE}/novas.sh"
}
: >"$ACME_FUNCTIONS"
for function_name in \
    install_cron_package verify_sysv_cron_boot_links enable_sysv_cron \
    start_cron_service cron_service_backend cron_service_is_active \
    is_exact_no_crontab_error verify_crontab_usable verify_acme_renewal_cron \
    ensure_acme_cron prepare_cloudflare_acme install_acme_payload; do
    extract_novas_function "$function_name" >>"$ACME_FUNCTIONS"
done
chmod 0644 "$ACME_FUNCTIONS"

ACME_CRONTAB_BEFORE="$WORK_DIR/runner-crontab-before"
read_runner_crontab "$ACME_CRONTAB_BEFORE" || die "cannot safely snapshot the runner user's existing crontab"
ACME_CRONTAB_WAS_PRESENT="$CRON_READ_PRESENT"
ACME_CRONTAB_READY=1

runuser -u "$RUNNER_USER" -- env -i \
    HOME="$ACME_USER_HOME" USER="$RUNNER_USER" LOGNAME="$RUNNER_USER" SHELL=/bin/bash \
    TMPDIR="$ACME_TMP" PATH="$ACME_SYSTEMCTL_BIN:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin" \
    /bin/bash -euo pipefail -c '
        source "$1"
        LOGE() { printf "error: %s\\n" "$*" >&2; }
        LOGI() { printf "info: %s\\n" "$*"; }
        release=ubuntu
        prepare_cloudflare_acme
        verify_acme_renewal_cron
        "$HOME/.acme.sh/acme.sh" --version
    ' acceptance "$ACME_FUNCTIONS"

ACME_CRONTAB_AFTER="$WORK_DIR/runner-crontab-after"
ACME_CRONTAB_WITHOUT_OWNED="$WORK_DIR/runner-crontab-without-owned"
read_runner_crontab "$ACME_CRONTAB_AFTER" || die "cannot read runner crontab after acme.sh setup"
[[ "$(count_owned_acme_jobs "$ACME_CRONTAB_AFTER")" == "1" ]] || die "prepare_cloudflare_acme did not install exactly one valid renewal job"
strip_owned_acme_jobs "$ACME_CRONTAB_AFTER" "$ACME_CRONTAB_WITHOUT_OWNED"
cmp -s "$ACME_CRONTAB_BEFORE" "$ACME_CRONTAB_WITHOUT_OWNED" || die "acme.sh changed unrelated runner crontab entries"
[[ -x "$ACME_USER_HOME/.acme.sh/acme.sh" ]] || die "official acme.sh was not installed in the temporary HOME"
printf 'PASS: extracted real ACME bootstrap installed official acme.sh and one owned cron job; no CA action was run\n'
