#!/usr/bin/env bash
set -Eeuo pipefail

readonly EXPECTED_JOB="test-acme-cron-linux"
readonly EXPECTED_WORKFLOW="发布 NovaPanel"
readonly DIAGNOSTICS_WORKFLOW="NovaPanel 安装验收"

die() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

require_github_hosted_release_job() {
    [[ "${NOVAS_ACME_CRON_OPT_IN:-}" == "1" ]] || die "explicit NOVAS_ACME_CRON_OPT_IN=1 is required"
    [[ "${NOVAS_GITHUB_ACTIONS:-}" == "true" ]] || die "GitHub Actions identity is missing"
    [[ "${NOVAS_RUNNER_ENVIRONMENT:-}" == "github-hosted" ]] || die "refusing non-hosted runners"
    [[ "${NOVAS_GITHUB_JOB:-}" == "$EXPECTED_JOB" ]] || die "refusing a non-dedicated workflow job"
    case "${NOVAS_GITHUB_WORKFLOW:-}" in
        "$EXPECTED_WORKFLOW" | "$DIAGNOSTICS_WORKFLOW") ;;
        *) die "refusing a different workflow" ;;
    esac
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

read_runner_crontab() {
    local destination="$1" error_file="${1}.stderr" status
    if LC_ALL=C crontab -u "$RUNNER_USER" -l >"$destination" 2>"$error_file"; then
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
    local original_status=$? cleanup_failed=0
    trap - EXIT INT TERM
    set +e

    if [[ "${ACME_CRONTAB_READY:-0}" == "1" ]]; then
        local current_cron="${WORK_DIR}/runner-crontab-current"
        local filtered_cron="${WORK_DIR}/runner-crontab-clean"
        local verified_cron="${WORK_DIR}/runner-crontab-verified"
        if read_runner_crontab "$current_cron"; then
            local current_present="$CRON_READ_PRESENT"
            if ! strip_owned_acme_jobs "$current_cron" "$filtered_cron"; then
                cleanup_failed=1
            elif [[ "$ACME_CRONTAB_WAS_PRESENT" == "0" && ! -s "$filtered_cron" ]]; then
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

    if [[ "$cleanup_failed" == 0 && -n "${WORK_DIR:-}" && -d "$WORK_DIR" ]]; then
        rm -rf "$WORK_DIR" || cleanup_failed=1
    fi
    if [[ "$cleanup_failed" != 0 ]]; then
        printf 'FAIL: ACME cron cleanup was incomplete; inspect this disposable runner before reuse\n' >&2
        [[ "$original_status" -ne 0 ]] || original_status=1
    fi
    exit "$original_status"
}

require_github_hosted_release_job

[[ $# -eq 0 ]] || die "usage: $0"
[[ -f /etc/os-release ]] || die "Ubuntu release identity is unavailable"
# shellcheck disable=SC1091
source /etc/os-release
[[ "${ID:-}" == "ubuntu" ]] || die "refusing to install packages outside Ubuntu"
command -v systemctl >/dev/null 2>&1 || die "systemctl is required for the native cron acceptance"

WORK_DIR=$(mktemp -d "$NOVAS_RUNNER_TEMP/novas-acme-cron.XXXXXX") || die "cannot create a runner-local temporary directory"
ACME_CRONTAB_READY=0
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
chmod 0755 "$WORK_DIR"

# Cron may reload systemd units here: this runner never hosts UFW/SSH acceptance.
export DEBIAN_FRONTEND=noninteractive
export NEEDRESTART_MODE=l
apt-get update || die "package index update failed"
apt-get install -y --no-upgrade --no-install-recommends cron curl || die "cron package installation failed"

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
