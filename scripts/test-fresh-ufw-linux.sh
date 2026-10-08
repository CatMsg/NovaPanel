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
    local cleanup_failed=0
    trap - EXIT INT TERM
    set +e

    if [[ -n "${SSHD_PID:-}" ]] && kill -0 "$SSHD_PID" 2>/dev/null; then
        kill "$SSHD_PID" 2>/dev/null || cleanup_failed=1
        wait "$SSHD_PID" 2>/dev/null || true
    fi

    if command -v ufw >/dev/null 2>&1; then
        status=$(LC_ALL=C ufw status 2>/dev/null)
        if [[ "$status" == Status:\ active* ]]; then
            LC_ALL=C ufw --force disable >/dev/null || cleanup_failed=1
        fi
        if [[ "$ALLOW_RULES_OWNED" == "1" ]]; then
            for port in "$SSH_PORT" "$PANEL_PORT" "$SUB_PORT"; do
                rule="ufw allow $port/tcp"
                while ufw_user_rules 2>/dev/null | grep -Fxq "$rule"; do
                    LC_ALL=C ufw --force delete allow "$port/tcp" >/dev/null || { cleanup_failed=1; break; }
                done
            done
        fi
        if [[ "$DENY_RULE_OWNED" == "1" ]]; then
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

    if [[ "${UFW_CONFIG_BACKUP_READY:-0}" == "1" ]]; then
        cp -p "${WORK_DIR}/ufw-default-before-test" /etc/default/ufw || cleanup_failed=1
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
        rm -f "$POLICY_RC" || cleanup_failed=1
    fi
    if [[ "${SSHD_CONFIG_CREATED:-0}" == "1" ]]; then
        rm -f /etc/ssh/sshd_config.d/00-novas-fresh-ufw-acceptance.conf || cleanup_failed=1
    fi
    if [[ "${SSHD_RUN_DIR_CREATED:-0}" == "1" ]]; then
        rmdir /run/sshd || cleanup_failed=1
    fi
    if [[ -n "${WORK_DIR:-}" && -d "$WORK_DIR" ]]; then
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
command -v ss >/dev/null 2>&1 || die "ss must already exist for the no-listener preflight"
command -v systemctl >/dev/null 2>&1 || die "systemctl is required for the native cron acceptance"

initial_listeners=$(LC_ALL=C ss -H -ltnp) || die "cannot inspect listeners before package installation"
if awk -v ssh_port="$SSH_PORT" -v panel_port="$PANEL_PORT" -v sub_port="$SUB_PORT" '
    {
        address = $4
        port = address
        sub(/^.*:/, "", port)
        if (index($0, "\"sshd\"") || index($0, "\"sshd-session\"") ||
            port == "22" || port == ssh_port || port == panel_port || port == sub_port) found = 1
    }
    END { exit !found }
' <<<"$initial_listeners"; then
    die "unexpected SSH or reserved-port listener exists; refusing package installation"
fi
if systemctl is-active --quiet ssh.socket || systemctl is-active --quiet sshd.socket; then
    die "an SSH socket unit is already active"
fi

if command -v ufw >/dev/null 2>&1; then
    initial_ufw_status=$(LC_ALL=C ufw status) || die "cannot read existing UFW state"
    case "$initial_ufw_status" in
        'Status: inactive'*) ;;
        'Status: active'*) die "refusing to run with pre-existing active UFW" ;;
        *) die "existing UFW state is ambiguous; refusing package installation" ;;
    esac
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
UFW_BASELINE_READY=0
UFW_CONFIG_BACKUP_READY=0
ACME_CRONTAB_READY=0
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

chmod 0755 "$WORK_DIR"
POLICY_RC_CREATED=1
printf '#!/bin/sh\nexit 101\n' >"$POLICY_RC"
chmod 0755 "$POLICY_RC"
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends ufw openssh-server openssh-client iproute2 cron
rm -f "$POLICY_RC"
POLICY_RC_CREATED=0
command -v crontab >/dev/null 2>&1 || die "the guarded cron package installation did not provide crontab"

post_install_listeners=$(LC_ALL=C ss -H -ltnp) || die "cannot inspect listeners after package installation"
if awk '
    index($0, "\"sshd\"") || index($0, "\"sshd-session\"") || $4 ~ /:22$/ { found = 1 }
    END { exit !found }
' <<<"$post_install_listeners"; then
    die "package installation started an unexpected SSH listener"
fi
[[ "$(LC_ALL=C ufw status | sed -n '1p')" == "Status: inactive" ]] || die "UFW is not inactive after package installation"
[[ -d /etc/ssh/sshd_config.d ]] || die "OpenSSH drop-in directory is unavailable"
grep -Eq '^[[:space:]]*Include[[:space:]]+/etc/ssh/sshd_config\.d/\*' /etc/ssh/sshd_config || die "default sshd configuration does not include drop-ins"
[[ ! -e /etc/ssh/sshd_config.d/00-novas-fresh-ufw-acceptance.conf && ! -L /etc/ssh/sshd_config.d/00-novas-fresh-ufw-acceptance.conf ]] || die "acceptance sshd drop-in already exists"
if [[ ! -d /run/sshd ]]; then
    mkdir -p /run/sshd
    SSHD_RUN_DIR_CREATED=1
fi

ufw_user_rules >"$WORK_DIR/ufw-rules-before-test" || die "cannot read the initial UFW user rules"
UFW_BASELINE_READY=1
for reserved_port in "$SSH_PORT" "$PANEL_PORT" "$SUB_PORT"; do
    if grep -Fq "$reserved_port/tcp" "$WORK_DIR/ufw-rules-before-test"; then
        die "initial UFW rules already use reserved test port $reserved_port"
    fi
done
if grep -Fq "from $DENY_SOURCE to any port $DENY_PORT proto tcp" "$WORK_DIR/ufw-rules-before-test"; then
    die "initial UFW rules collide with the acceptance deny-rule fixture"
fi
if [[ -f /etc/default/ufw ]]; then
    cp -p /etc/default/ufw "$WORK_DIR/ufw-default-before-test"
    UFW_CONFIG_BACKUP_READY=1
else
    die "UFW default configuration is missing"
fi

ssh-keygen -q -t ed25519 -N '' -f "$WORK_DIR/ssh_host_ed25519_key"
SSHD_CONFIG_CREATED=1
cat >/etc/ssh/sshd_config.d/00-novas-fresh-ufw-acceptance.conf <<EOF
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
sshd_effective=$(LC_ALL=C sshd -T) || die "sshd rejected the temporary listener configuration"
port_count=$(awk '$1 == "port" { count++ } END { print count + 0 }' <<<"$sshd_effective")
[[ "$port_count" == "1" ]] || die "sshd effective configuration contains unexpected ports"
awk -v port="$SSH_PORT" '
    $1 == "port" && $2 == port { found_port = 1 }
    $1 == "listenaddress" {
        address = $2
        if (address !~ /:2222$/ || (address !~ /127\.0\.0\.1/ && address !~ /::1/)) exit 1
        count++
    }
    END { exit !(found_port && count == 2) }
' <<<"$sshd_effective" || die "sshd is not restricted to IPv4/IPv6 loopback on port $SSH_PORT"
sshd -t
sshd -D -E "$WORK_DIR/sshd.log" &
SSHD_PID=$!
for attempt in {1..50}; do
    kill -0 "$SSHD_PID" 2>/dev/null || { cat "$WORK_DIR/sshd.log" >&2; die "temporary sshd exited before listening"; }
    if LC_ALL=C ss -H -ltnp | awk -v port="$SSH_PORT" '
        $4 ~ (":" port "$" ) && index($0, "\"sshd\"") { found++ }
        END { exit !(found == 2) }
    '; then
        break
    fi
    sleep 0.1
done
LC_ALL=C ss -H -ltnp | awk -v port="$SSH_PORT" '
    $4 ~ (":" port "$" ) && index($0, "\"sshd\"") { found++ }
    END { exit !(found == 2) }
' || { cat "$WORK_DIR/sshd.log" >&2; die "actual sshd did not bind both loopback address families"; }

mkdir -p "$WORK_DIR/panel-home"
cp "$TEST_BINARY" "$WORK_DIR/panel-home/novas"
chmod 0755 "$WORK_DIR/panel-home/novas"
NOVAS_DB_FOLDER="$WORK_DIR/panel-home/db" "$WORK_DIR/panel-home/novas" setting -show
[[ -s "$WORK_DIR/panel-home/db/novas.db" ]] || die "the test-built novas CLI did not create its temporary database"

source "${NOVAS_WORKSPACE}/install.sh"
release=ubuntu
DENY_RULE_OWNED=1
ufw --force deny from "$DENY_SOURCE" to any port "$DENY_PORT" proto tcp
grep -Fxq "ufw deny from $DENY_SOURCE to any port $DENY_PORT proto tcp" <(ufw_user_rules) || die "could not establish the unrelated deny-rule fixture"

ALLOW_RULES_OWNED=1
install_fresh_ufw "$WORK_DIR/panel-home" "" "$PANEL_PORT" "$SUB_PORT"

ufw_status=$(LC_ALL=C ufw status) || die "cannot read UFW status after the installer hook"
[[ "$ufw_status" == *$'Status: active'* ]] || die "UFW is not active after the installer hook"
grep -Eq '^IPV6=yes$' /etc/default/ufw || die "UFW IPv6 is not enabled"
for port in "$SSH_PORT" "$PANEL_PORT" "$SUB_PORT"; do
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
    [[ -z "$baseline_rule" ]] || grep -Fxq "$baseline_rule" <(ufw_user_rules) || die "an initial UFW rule was not preserved: $baseline_rule"
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
