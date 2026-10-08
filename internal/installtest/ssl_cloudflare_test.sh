#!/bin/bash
set -euo pipefail

PATH=''
unset BASH_ENV ENV
repo_root="$(cd "$(/usr/bin/dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source_file="${repo_root}/novas.sh"
test_root="$(/usr/bin/mktemp -d)"
trap '/bin/rm -rf "${test_root}"' EXIT

# Only file-processing utilities are reachable, even after an init mock is removed.
utility_path="${test_root}/bin"
/bin/mkdir -p "${utility_path}"
for utility in awk cat chmod cp cut dirname grep head ln mkdir mktemp mv rm rmdir stat tr; do
    utility_source=''
    for directory in /usr/bin /bin; do
        if [[ -x "${directory}/${utility}" ]]; then
            utility_source="${directory}/${utility}"
            break
        fi
    done
    [[ -n "${utility_source}" ]] || { printf 'Missing allowed utility: %s\n' "${utility}" >&2; exit 1; }
    /bin/ln -s "${utility_source}" "${utility_path}/${utility}"
done
PATH="${utility_path}"
export PATH

extract_function() {
    awk -v function_name="$1" '
        $0 == function_name "() {" { in_function = 1 }
        in_function { print }
        in_function && $0 == "}" { exit }
    ' "${source_file}"
}

extracted_functions="${test_root}/extracted-functions.sh"
: > "${extracted_functions}"
for function_name in \
    install_cron_package verify_sysv_cron_boot_links enable_sysv_cron \
    start_cron_service cron_service_backend cron_service_is_active \
    is_exact_no_crontab_error verify_crontab_usable verify_acme_renewal_cron \
    ensure_acme_cron prepare_cloudflare_acme install_acme_payload install_acme \
    resolve_acme_cert_files validate_cf_domain validate_cf_account_id validate_cf_token \
    issue_cloudflare_certificate run_cloudflare_acme_issue \
    restore_cloudflare_certificate_backup install_cloudflare_certificate; do
    extract_function "${function_name}" >> "${extracted_functions}"
done
source "${extracted_functions}"
# Preserve the real link verifier; only its /etc paths are redirected into the fixture.
link_verifier="$(extract_function verify_sysv_cron_boot_links)"
eval "${link_verifier/verify_sysv_cron_boot_links()/fixture_verify_sysv_cron_boot_links()}"
verify_sysv_cron_boot_links() {
    [[ "$2" == /etc/rc[2345].d ]] || return 1
    fixture_verify_sysv_cron_boot_links "$1" "${SYSV_ROOT}/${2#/etc/}"
}

fail() {
    printf 'FAIL: %s\n' "$1" >&2
    exit 1
}

assert() {
    "$@" || fail "assertion failed: $*"
}

assert_file_contains() {
    grep -Fq -- "$1" "$2" || fail "expected fixture content was absent from $2"
}

assert_file_lacks() {
    if grep -Fq -- "$1" "$2"; then
        fail "unexpected fixture content was present in $2"
    fi
}

event() {
    printf '%s\n' "$1" >> "${EVENT_LOG}"
}

rm() {
    if [[ "${SCENARIO:-}" == "rollback-delete-fail" || "${SCENARIO:-}" == "rollback-delete-noop" ]] &&
        [[ "$*" == "-rf ${CERT_DIR:-}" ]]; then
        event 'rollback:delete'
        [[ "${SCENARIO}" == "rollback-delete-noop" ]]
        return
    fi
    command rm "$@"
}

mv() {
    # Model GNU mv -T on macOS too: an existing destination can never absorb the source.
    [[ "${1:-}" == "-T" && "${2:-}" == "--" && $# == 4 ]] || return 2
    local source="$3" destination="$4"
    if [[ "${source}" == *".old."* ]]; then
        event 'rollback:restore'
        [[ "${SCENARIO}" != "rollback-restore-fail" ]] || return 1
    fi
    if [[ "${source}" == *".new."* && "${SCENARIO}" == "publish-fail" ]]; then
        return 1
    fi
    [[ ! -e "${destination}" && ! -L "${destination}" ]] || return 1
    command mv -- "${source}" "${destination}"
}

LOGE() { printf 'error: %s\n' "$*" >&2; }
LOGI() { printf 'info: %s\n' "$*"; }
LOGD() { printf 'debug: %s\n' "$*"; }

apt-get() {
    event "pkg:apt-get:$*"
    [[ "${SCENARIO}" != "package-fail" ]]
}

dnf() {
    event "pkg:dnf:$*"
    [[ "${SCENARIO}" != "package-fail" ]]
}

yum() {
    event "pkg:yum:$*"
    [[ "${SCENARIO}" != "package-fail" ]]
}

pacman() {
    event "pkg:pacman:$*"
    [[ "${SCENARIO}" != "package-fail" ]]
}

systemctl() {
    event "systemctl:$*"
    case "$1" in
        enable)
            [[ "${SCENARIO}" != "service-start-fail" ]] || return 1
            SERVICE_ACTIVE=1
            ;;
        is-active)
            [[ "${SERVICE_ACTIVE}" == "1" && "${SCENARIO}" != "service-inactive" ]]
            ;;
        *) return 1 ;;
    esac
}

service() {
    event "service:$*"
    case "$2" in
        start)
            [[ "${SCENARIO}" != "service-start-fail" ]] || return 1
            SERVICE_ACTIVE=1
            ;;
        status)
            [[ "${SERVICE_ACTIVE}" == "1" ]]
            ;;
        *) return 1 ;;
    esac
}

rc-update() {
    event "rc-update:$*"
    [[ "${SCENARIO}" != "service-start-fail" ]]
}

rc-service() {
    event "rc-service:$*"
    case "$2" in
        start)
            [[ "${SCENARIO}" != "service-start-fail" ]] || return 1
            SERVICE_ACTIVE=1
            ;;
        status)
            [[ "${SERVICE_ACTIVE}" == "1" ]]
            ;;
        *) return 1 ;;
    esac
}

write_sysv_boot_links() {
    local service_name="$1" runlevel=""
    [[ "${SCENARIO}" != "sysv-enable-fail" ]] || return 1
    [[ "${SCENARIO}" != "sysv-enable-no-links" ]] || return 0
    mkdir -p "${SYSV_ROOT}/init.d"
    printf '#!/bin/sh\nexit 0\n' > "${SYSV_ROOT}/init.d/${service_name}"
    chmod 700 "${SYSV_ROOT}/init.d/${service_name}"
    for runlevel in 2 3 4 5; do
        [[ "${SCENARIO}" != "sysv-enable-partial" || "${runlevel}" == 2 ]] || continue
        mkdir -p "${SYSV_ROOT}/rc${runlevel}.d"
        if [[ "${SCENARIO}" == "sysv-broken-link" ]]; then
            ln -sf ../init.d/missing "${SYSV_ROOT}/rc${runlevel}.d/S01${service_name}"
        else
            ln -sf "../init.d/${service_name}" "${SYSV_ROOT}/rc${runlevel}.d/S01${service_name}"
        fi
    done
}

update-rc.d() {
    event "update-rc.d:$*"
    [[ $# == 2 ]] || return 2
    case "$2" in
        defaults) [[ "${SCENARIO}" != "sysv-defaults-fail" ]] ;;
        enable) write_sysv_boot_links "$1" ;;
        *) return 2 ;;
    esac
}

chkconfig() {
    event "chkconfig:$*"
    case "$*" in
        '--add crond') [[ "${SCENARIO}" != "sysv-defaults-fail" ]] ;;
        '--level 2345 crond on') write_sysv_boot_links crond ;;
        *) return 2 ;;
    esac
}

crontab() {
    event "crontab:$*"
    [[ "${1:-}" == "-l" ]] || return 2
    case "${CRONTAB_MODE}" in
        permission-error)
            printf '%s\n' "can't open /var/spool/cron/root" >&2
            return 1
            ;;
        missing)
            if [[ -f "${CRON_FILE}" ]]; then
                /bin/cat "${CRON_FILE}"
            else
                printf 'no crontab for root\n' >&2
                return 1
            fi
            ;;
        file)
            [[ -f "${CRON_FILE}" ]] || {
                printf 'no crontab for root\n' >&2
                return 1
            }
            /bin/cat "${CRON_FILE}"
            ;;
        *) return 2 ;;
    esac
}

reset_case() {
    local name="$1"
    local scenario="$2"
    local case_root="${test_root}/${name}"

    mkdir -p "${case_root}/home" "${case_root}/systemd"
    HOME="${case_root}/home"
    TMPDIR="${test_root}"
    EVENT_LOG="${case_root}/events"
    CRON_FILE="${case_root}/crontab"
    ACME_LOG="${case_root}/acme-events"
    ACME_ARGS_FILE="${case_root}/acme-args"
    ACME_ENV_FILE="${case_root}/acme-env"
    ACME_CONFIG_FILE="${case_root}/acme-install-config"
    NOVAS_SYSTEMD_RUNTIME_DIR="${case_root}/systemd"
    SCENARIO="${scenario}"
    CRONTAB_MODE="missing"
    SERVICE_ACTIVE=0
    SYSV_ROOT="${case_root}/sysv"
    CERT_DIR=''
    release=ubuntu
    : > "${EVENT_LOG}"
    export HOME TMPDIR EVENT_LOG CRON_FILE ACME_LOG ACME_ARGS_FILE ACME_ENV_FILE
    export ACME_CONFIG_FILE NOVAS_SYSTEMD_RUNTIME_DIR SCENARIO CRONTAB_MODE
}

write_expected_crontab() {
    printf '%s\n' \
        '17 3 * * * /usr/local/bin/keep-existing-job' \
        "15 4 * * * \"${HOME}/.acme.sh\"/acme.sh --cron --home \"${HOME}/.acme.sh\" > /dev/null" \
        > "${CRON_FILE}"
    CRONTAB_MODE=file
}

write_acme_stub() {
    mkdir -p "${HOME}/.acme.sh"
    cat > "${HOME}/.acme.sh/acme.sh" <<'ACME_STUB'
#!/bin/bash
set -u
printf 'acme:%s\n' "${1:-}" >> "${EVENT_LOG}"
case "${1:-}" in
    --install-cronjob)
        [[ "${SCENARIO}" != "acme-cron-fail" ]] || exit 1
        if [[ "${SCENARIO}" != "acme-cron-noentry" ]] &&
            ! grep -Fq -- 'acme.sh --cron --home' "${CRON_FILE}" 2>/dev/null; then
            printf '%s\n' "15 4 * * * \"${HOME}/.acme.sh\"/acme.sh --cron --home \"${HOME}/.acme.sh\" > /dev/null" >> "${CRON_FILE}"
        fi
        ;;
    --set-default-ca)
        [[ "${SCENARIO}" != "set-ca-fail" ]] || exit 1
        ;;
    --issue)
        printf 'token=%s\naccount=%s\nkey=%s\nemail=%s\nzone=%s\n' \
            "${CF_Token-<unset>}" "${CF_Account_ID-<unset>}" "${CF_Key-<unset>}" \
            "${CF_Email-<unset>}" "${CF_Zone_ID-<unset>}" > "${ACME_ENV_FILE}"
        printf '%s\n' "$@" > "${ACME_ARGS_FILE}"
        [[ "${SCENARIO}" != "issue-fail" ]] || exit 1
        ;;
    --upgrade)
        [[ "${SCENARIO}" != "upgrade-fail" ]] || exit 1
        ;;
    --installcert)
        [[ "${SCENARIO}" != "installcert-fail" ]] || exit 1
        fullchain=''
        keyfile=''
        while (($#)); do
            case "$1" in
                --fullchain-file) fullchain="$2"; shift 2 ;;
                --key-file) keyfile="$2"; shift 2 ;;
                *) shift ;;
            esac
        done
        printf 'fullchain=%s\nkey=%s\n' "$fullchain" "$keyfile" > "${ACME_CONFIG_FILE}"
        printf 'new certificate\n' > "$fullchain"
        case "${SCENARIO}" in
            installcert-partial-fail | rollback-delete-fail | rollback-delete-noop | rollback-restore-fail)
                exit 1 ;;
        esac
        printf 'new private key\n' > "$keyfile"
        ;;
    --simulate-renewal)
        fullchain=''
        keyfile=''
        while IFS='=' read -r name value; do
            case "$name" in
                fullchain) fullchain="$value" ;;
                key) keyfile="$value" ;;
            esac
        done < "${ACME_CONFIG_FILE}"
        printf 'renewed certificate\n' > "$fullchain"
        printf 'renewed private key\n' > "$keyfile"
        ;;
    *) exit 2 ;;
esac
ACME_STUB
    chmod 700 "${HOME}/.acme.sh/acme.sh"
}

install_acme_payload() {
    event "install-acme-payload"
    [[ "${SCENARIO}" != "payload-fail" ]] || return 1
    write_acme_stub
}

count_events() {
    awk -v needle="$1" 'index($0, needle) { count++ } END { print count + 0 }' "${EVENT_LOG}"
}

assert_event_before() {
    local first second
    first=$(grep -nF -- "$1" "${EVENT_LOG}" | head -n 1 | cut -d: -f1) || fail "missing first event"
    second=$(grep -nF -- "$2" "${EVENT_LOG}" | head -n 1 | cut -d: -f1) || fail "missing second event"
    ((first < second)) || fail "events were out of order"
}

assert_eq() {
    [[ "$1" == "$2" ]] || fail "$3"
}

seed_certificate_case() {
    reset_case "$1" "$2"
    write_acme_stub
    CERT_DIR="${HOME}/certs/example.com"
    mkdir -p "${HOME}/.acme.sh/example.com_ecc" "${CERT_DIR}"
    printf 'issued certificate\n' > "${HOME}/.acme.sh/example.com_ecc/fullchain.cer"
    printf 'issued private key\n' > "${HOME}/.acme.sh/example.com_ecc/example.com.key"
    printf 'old certificate\n' > "${CERT_DIR}/fullchain.pem"
    printf 'old private key\n' > "${CERT_DIR}/privkey.pem"
    chmod 600 "${CERT_DIR}/privkey.pem"
}

# The menu is not sourced or executed; verify its prompt and preflight order statically.
menu_source="$(extract_function ssl_cert_issue_CF)"
preflight_line="$(printf '%s\n' "${menu_source}" | grep -nF 'if ! prepare_cloudflare_acme' | cut -d: -f1)"
domain_prompt_line="$(printf '%s\n' "${menu_source}" | grep -nF 'read -r -p "请在此输入域名： "' | cut -d: -f1)"
account_prompt_line="$(printf '%s\n' "${menu_source}" | grep -nF 'read -r -p "请输入 Cloudflare Account ID' | cut -d: -f1)"
token_prompt_line="$(printf '%s\n' "${menu_source}" | grep -nF 'read -r -s -p' | cut -d: -f1)"
issue_line="$(printf '%s\n' "${menu_source}" | grep -nF 'run_cloudflare_acme_issue' | cut -d: -f1)"
((preflight_line < domain_prompt_line && domain_prompt_line < account_prompt_line && account_prompt_line < token_prompt_line && token_prompt_line < issue_line)) || fail "menu preflight or credential prompt order changed"
[[ "${menu_source}" != *"CF_GlobalKey"* && "${menu_source}" != *"全局 API Key"* && "${menu_source}" != *"请输入 key"* && "${menu_source}" != *"注册邮箱"* && "${menu_source}" != *"rm -rf"* ]] || fail "legacy key/email prompt or certificate-tree deletion returned to the menu"
[[ "${menu_source}" == *"Zone > DNS > Edit"* && "${menu_source}" == *"Zone > Zone > Read"* && "${menu_source}" == *"仅授权目标 Zone"* ]] || fail "Cloudflare token permission guidance is incomplete"

assert validate_cf_domain "example.co.uk"
assert validate_cf_domain "a1.example-2.net"
if validate_cf_domain "*.example.com" || validate_cf_domain "-bad.example.com" || validate_cf_domain "bad..example.com" || validate_cf_domain "localhost" || validate_cf_domain "example.com/path"; then
    fail "invalid domain accepted"
fi
long_label="$(printf '%064d' 0 | tr '0' a)"
if validate_cf_domain "${long_label}.example"; then fail "overlong DNS label accepted"; fi
assert validate_cf_account_id "0123456789abcdef0123456789abcdef"
if validate_cf_account_id "0123456789abcdef0123456789abcdeg"; then fail "non-hex account id accepted"; fi
if validate_cf_account_id "0123456789abcdef0123456789abcde"; then fail "short account id accepted"; fi
assert validate_cf_token "test-token_123.abc"
if validate_cf_token "token with space" || validate_cf_token ""; then fail "invalid token accepted"; fi

reset_case crontab-errors healthy
CRONTAB_MODE=missing
assert verify_crontab_usable
CRONTAB_MODE=permission-error
if verify_crontab_usable 1; then fail "generic can't-open error accepted as an empty crontab"; fi

reset_case cron-idempotency healthy
write_expected_crontab
before_crontab="$(/bin/cat "${CRON_FILE}")"
SERVICE_ACTIVE=1
ensure_acme_cron
assert_eq "$(count_events 'pkg:')" 0 "healthy cron invoked a package manager"
assert_eq "$(/bin/cat "${CRON_FILE}")" "${before_crontab}" "existing crontab was changed"

for distro in ubuntu debian armbian centos almalinux rocky oracle fedora arch manjaro parch; do
    reset_case "distro-${distro}" package-install
    release="${distro}"
    ensure_acme_cron
    case "${distro}" in
        ubuntu | debian | armbian) expected_pkg='pkg:apt-get:install -y --no-install-recommends cron'; expected_unit='cron' ;;
        centos | almalinux | rocky | oracle | fedora) expected_pkg='pkg:dnf:install -y cronie'; expected_unit='crond' ;;
        arch | manjaro | parch) expected_pkg='pkg:pacman:-S --noconfirm cronie'; expected_unit='cronie' ;;
    esac
    assert_file_contains "${expected_pkg}" "${EVENT_LOG}"
    assert_file_contains "${expected_unit}" "${EVENT_LOG}"
done

reset_case first-cloudflare-setup setup
prepare_cloudflare_acme
assert_eq "$(count_events 'pkg:apt-get:')" 1 "Cloudflare preflight installed cron more than once"
assert_eq "$(count_events 'install-acme-payload')" 1 "Cloudflare preflight installed acme.sh more than once"
assert_eq "$(count_events 'acme:--install-cronjob')" 1 "missing renewal job was not installed exactly once"
assert_event_before 'pkg:apt-get:' 'install-acme-payload'
assert_event_before 'install-acme-payload' 'acme:--install-cronjob'
assert verify_acme_renewal_cron

reset_case existing-acme-no-renew-job setup
write_acme_stub
prepare_cloudflare_acme
assert_eq "$(count_events 'install-acme-payload')" 0 "existing acme.sh was reinstalled"
assert_eq "$(count_events 'acme:--install-cronjob')" 1 "existing acme.sh without a job was not repaired"
assert verify_acme_renewal_cron

reset_case existing-renew-job healthy
write_acme_stub
write_expected_crontab
SERVICE_ACTIVE=1
prepare_cloudflare_acme
assert_eq "$(count_events 'pkg:')" 0 "healthy cron invoked a package manager"
assert_eq "$(count_events 'acme:--install-cronjob')" 0 "existing renewal job was duplicated"

reset_case commented-renew-job acme-cron-noentry
write_acme_stub
printf '%s\n' "# 15 4 * * * \"${HOME}/.acme.sh\"/acme.sh --cron --home \"${HOME}/.acme.sh\" > /dev/null" > "${CRON_FILE}"
CRONTAB_MODE=file
if prepare_cloudflare_acme; then fail "commented renewal entry satisfied preflight"; fi
assert_eq "$(count_events 'acme:--install-cronjob')" 1 "commented renewal entry did not trigger repair"
assert_file_lacks 'acme:--issue' "${EVENT_LOG}"

reset_case wrong-home-renew-job acme-cron-noentry
write_acme_stub
printf '%s\n' '15 4 * * * "/elsewhere/.acme.sh"/acme.sh --cron --home "/elsewhere/.acme.sh" > /dev/null' > "${CRON_FILE}"
CRONTAB_MODE=file
if prepare_cloudflare_acme; then fail "a different acme.sh home satisfied preflight"; fi
assert_file_lacks 'acme:--issue' "${EVENT_LOG}"

reset_case wrong-script-prefix acme-cron-noentry
write_acme_stub
printf '%s\n' "15 4 * * * \"/wrong${HOME}/.acme.sh\"/acme.sh --cron --home \"${HOME}/.acme.sh\" > /dev/null" > "${CRON_FILE}"
CRONTAB_MODE=file
if verify_acme_renewal_cron 1; then fail "a prefixed wrong acme.sh path satisfied verification"; fi
if prepare_cloudflare_acme; then fail "a prefixed wrong acme.sh path satisfied preflight"; fi
assert_file_lacks 'acme:--issue' "${EVENT_LOG}"

reset_case package-failure package-fail
if prepare_cloudflare_acme; then fail "package installation failure was accepted"; fi
assert_file_lacks 'install-acme-payload' "${EVENT_LOG}"
assert_file_lacks 'acme:--issue' "${EVENT_LOG}"

reset_case install-acme-preflight package-fail
if install_acme; then fail "standalone acme install bypassed cron failure"; fi
assert_file_lacks 'install-acme-payload' "${EVENT_LOG}"

reset_case service-failure service-start-fail
write_acme_stub
if prepare_cloudflare_acme; then fail "inactive cron service was accepted"; fi
assert_file_lacks 'acme:--install-cronjob' "${EVENT_LOG}"
assert_file_lacks 'acme:--issue' "${EVENT_LOG}"

reset_case unreadable-crontab healthy
CRONTAB_MODE=permission-error
if ensure_acme_cron; then fail "unreadable crontab was accepted"; fi
assert_eq "$(count_events 'pkg:')" 0 "unreadable crontab triggered package installation"

reset_case acme-cron-failure acme-cron-fail
write_acme_stub
if prepare_cloudflare_acme; then fail "acme renewal installation failure was accepted"; fi
assert_file_lacks 'acme:--issue' "${EVENT_LOG}"

reset_case account-scope healthy
write_acme_stub
token_for_test='test-token_123.abc'
account_for_test='0123456789abcdef0123456789abcdef'
export CF_Token='parent-token-sentinel'
export CF_Account_ID='parent-account-sentinel'
export CF_Key='parent-key-sentinel'
export CF_Email='parent-email-sentinel'
export CF_Zone_ID='parent-zone-sentinel'
parent_environment="${CF_Token}|${CF_Account_ID}|${CF_Key}|${CF_Email}|${CF_Zone_ID}"
issue_output="$(issue_cloudflare_certificate "${HOME}/.acme.sh/acme.sh" "example.com" "${token_for_test}" "${account_for_test}" 1 2>&1)"
[[ "${issue_output}" != *"${token_for_test}"* && "${issue_output}" != *"parent-key-sentinel"* ]] || fail "credential appeared in command output"
assert_eq "${CF_Token}|${CF_Account_ID}|${CF_Key}|${CF_Email}|${CF_Zone_ID}" "${parent_environment}" "parent Cloudflare environment changed"
assert_file_contains "token=${token_for_test}" "${ACME_ENV_FILE}"
assert_file_contains "account=${account_for_test}" "${ACME_ENV_FILE}"
assert_file_contains 'key=<unset>' "${ACME_ENV_FILE}"
assert_file_contains 'email=<unset>' "${ACME_ENV_FILE}"
assert_file_contains 'zone=<unset>' "${ACME_ENV_FILE}"
assert_file_contains '*.example.com' "${ACME_ARGS_FILE}"
assert_file_contains '--force' "${ACME_ARGS_FILE}"
unset CF_Token CF_Account_ID CF_Key CF_Email CF_Zone_ID

reset_case issue-failure-preserves-cert issue-fail
write_acme_stub
cert_root="${test_root}/issue-failure-certs"
mkdir -p "${cert_root}/example.com"
printf 'old certificate\n' > "${cert_root}/example.com/fullchain.pem"
printf 'old private key\n' > "${cert_root}/example.com/privkey.pem"
if run_cloudflare_acme_issue example.com test-token_123.abc 0123456789abcdef0123456789abcdef 0; then
    fail "failing ACME issue was accepted"
fi
assert_eq "$(/bin/cat "${cert_root}/example.com/fullchain.pem")" 'old certificate' "issue failure changed the old certificate"
assert_file_lacks 'test-token_123.abc' "${EVENT_LOG}"

reset_case installcert-failure-preserves-cert installcert-fail
write_acme_stub
mkdir -p "${HOME}/.acme.sh/example.com_ecc" "${test_root}/installcert-failure-certs/example.com"
printf 'issued certificate\n' > "${HOME}/.acme.sh/example.com_ecc/fullchain.cer"
printf 'issued private key\n' > "${HOME}/.acme.sh/example.com_ecc/example.com.key"
printf 'old certificate\n' > "${test_root}/installcert-failure-certs/example.com/fullchain.pem"
printf 'old private key\n' > "${test_root}/installcert-failure-certs/example.com/privkey.pem"
chmod 600 "${test_root}/installcert-failure-certs/example.com/privkey.pem"
if install_cloudflare_certificate "${HOME}/.acme.sh/acme.sh" example.com "${test_root}/installcert-failure-certs"; then
    fail "failing cert install was accepted"
fi
assert_eq "$(/bin/cat "${test_root}/installcert-failure-certs/example.com/fullchain.pem")" 'old certificate' "install failure lost the old certificate"
assert_eq "$(/bin/cat "${test_root}/installcert-failure-certs/example.com/privkey.pem")" 'old private key' "install failure lost the old private key"

for scenario in installcert-partial-fail publish-fail; do
    seed_certificate_case "cert-${scenario}" "${scenario}"
    if install_cloudflare_certificate "${HOME}/.acme.sh/acme.sh" example.com "${HOME}/certs"; then
        fail "certificate failure was accepted"
    fi
    assert_eq "$(/bin/cat "${CERT_DIR}/fullchain.pem")" 'old certificate' "certificate failure did not restore the old certificate"
    assert_eq "$(/bin/cat "${CERT_DIR}/privkey.pem")" 'old private key' "certificate failure did not restore the old key"
done

for scenario in rollback-delete-fail rollback-delete-noop rollback-restore-fail; do
    seed_certificate_case "cert-${scenario}" "${scenario}"
    if install_cloudflare_certificate "${HOME}/.acme.sh/acme.sh" example.com "${HOME}/certs" > "${HOME}/failure-output" 2>&1; then
        fail "failed rollback was accepted"
    fi
    backups=("${HOME}/certs"/.example.com.old.*)
    [[ ${#backups[@]} == 1 && -d "${backups[0]}" ]] || fail "rollback failure lost the backup"
    assert_eq "$(/bin/cat "${backups[0]}/fullchain.pem")" 'old certificate' "backup certificate was changed"
    assert_eq "$(/bin/cat "${backups[0]}/privkey.pem")" 'old private key' "backup key was changed"
    assert_file_contains "${backups[0]}" "${HOME}/failure-output"
    assert_file_lacks '已恢复旧证书' "${HOME}/failure-output"
    [[ ! -e "${CERT_DIR}/${backups[0]##*/}" ]] || fail "backup was nested in the destination"
    if [[ "${scenario}" != rollback-restore-fail ]]; then
        assert_eq "$(count_events 'rollback:restore')" 0 "restoration ran with an uncleared destination"
    fi
done

seed_certificate_case occupied-rollback-target healthy
occupied_backup="${HOME}/certs/.example.com.old.fixture"
command mv -- "${CERT_DIR}" "${occupied_backup}"
mkdir -p "${CERT_DIR}"
if restore_cloudflare_certificate_backup "${occupied_backup}" "${CERT_DIR}" > "${HOME}/failure-output" 2>&1; then
    fail "occupied rollback destination was accepted"
fi
assert_eq "$(/bin/cat "${occupied_backup}/fullchain.pem")" 'old certificate' "occupied destination lost the backup"
assert_eq "$(count_events 'rollback:restore')" 0 "occupied destination reached mv"
assert_file_lacks '已恢复旧证书' "${HOME}/failure-output"

seed_certificate_case no-old-cert installcert-partial-fail
command rm -rf "${CERT_DIR}"
if install_cloudflare_certificate "${HOME}/.acme.sh/acme.sh" example.com "${HOME}/certs" > "${HOME}/failure-output" 2>&1; then
    fail "first installation failure was accepted"
fi
assert_file_lacks '已恢复旧证书' "${HOME}/failure-output"

reset_case stable-renewal-target success
write_acme_stub
mkdir -p "${HOME}/.acme.sh/example.com_ecc" "${test_root}/stable-certs/example.com"
printf 'issued certificate\n' > "${HOME}/.acme.sh/example.com_ecc/fullchain.cer"
printf 'issued private key\n' > "${HOME}/.acme.sh/example.com_ecc/example.com.key"
printf 'old certificate\n' > "${test_root}/stable-certs/example.com/fullchain.pem"
printf 'old private key\n' > "${test_root}/stable-certs/example.com/privkey.pem"
chmod 600 "${test_root}/stable-certs/example.com/privkey.pem"
install_cloudflare_certificate "${HOME}/.acme.sh/acme.sh" example.com "${test_root}/stable-certs"
assert_file_contains "fullchain=${test_root}/stable-certs/example.com/fullchain.pem" "${ACME_CONFIG_FILE}"
assert_file_contains "key=${test_root}/stable-certs/example.com/privkey.pem" "${ACME_CONFIG_FILE}"
key_mode="$(stat -c '%a' "${test_root}/stable-certs/example.com/privkey.pem" 2>/dev/null || stat -f '%Lp' "${test_root}/stable-certs/example.com/privkey.pem")"
assert_eq "${key_mode}" 600 "private key permissions were unsafe"
"${HOME}/.acme.sh/acme.sh" --simulate-renewal
assert_eq "$(/bin/cat "${test_root}/stable-certs/example.com/fullchain.pem")" 'renewed certificate' "renewal did not update the stable cert path"
assert_eq "$(/bin/cat "${test_root}/stable-certs/example.com/privkey.pem")" 'renewed private key' "renewal did not update the stable key path"

reset_case openrc-backend package-install
rmdir "${NOVAS_SYSTEMD_RUNTIME_DIR}"
ensure_acme_cron
assert_file_contains 'rc-update:add cron default' "${EVENT_LOG}"
assert_file_contains 'rc-service:cron start' "${EVENT_LOG}"

reset_case service-backend package-install
rmdir "${NOVAS_SYSTEMD_RUNTIME_DIR}"
unset -f systemctl rc-service rc-update
write_expected_crontab
before_crontab="$(/bin/cat "${CRON_FILE}")"
ensure_acme_cron
assert_file_contains 'update-rc.d:cron defaults' "${EVENT_LOG}"
assert_file_contains 'update-rc.d:cron enable' "${EVENT_LOG}"
assert_file_contains 'service:cron start' "${EVENT_LOG}"
assert_file_contains 'service:cron status' "${EVENT_LOG}"
assert_event_before 'update-rc.d:cron enable' 'service:cron start'
assert_eq "$(/bin/cat "${CRON_FILE}")" "${before_crontab}" "SysV setup changed prior jobs"
SERVICE_ACTIVE=1
ensure_acme_cron
assert_eq "$(count_events 'pkg:')" 1 "healthy SysV cron reinstalled its package"

for distro in centos almalinux rocky oracle fedora; do
    reset_case "sysv-${distro}" package-install
    release="${distro}"
    rmdir "${NOVAS_SYSTEMD_RUNTIME_DIR}"
    ensure_acme_cron
    assert_file_contains 'chkconfig:--add crond' "${EVENT_LOG}"
    assert_file_contains 'chkconfig:--level 2345 crond on' "${EVENT_LOG}"
    assert_event_before 'chkconfig:--level 2345 crond on' 'service:crond start'
done

for scenario in sysv-defaults-fail sysv-enable-fail sysv-enable-no-links sysv-enable-partial sysv-broken-link; do
    for distro in debian centos; do
        reset_case "${distro}-${scenario}" "${scenario}"
        release="${distro}"
        rmdir "${NOVAS_SYSTEMD_RUNTIME_DIR}"
        write_acme_stub
        write_expected_crontab
        before_crontab="$(/bin/cat "${CRON_FILE}")"
        if prepare_cloudflare_acme; then fail "unverified SysV enablement was accepted"; fi
        assert_file_lacks ' start' "${EVENT_LOG}"
        assert_file_lacks 'acme:' "${EVENT_LOG}"
        assert_eq "$(/bin/cat "${CRON_FILE}")" "${before_crontab}" "failed SysV setup changed prior jobs"
    done
done

reset_case unsupported-sysv package-install
release=arch
rmdir "${NOVAS_SYSTEMD_RUNTIME_DIR}"
if ensure_acme_cron; then fail "unsupported SysV enablement was accepted"; fi
assert_file_lacks ' start' "${EVENT_LOG}"

unset -f update-rc.d chkconfig
for distro in debian centos; do
    reset_case "missing-sysv-${distro}" healthy
    release="${distro}"
    rmdir "${NOVAS_SYSTEMD_RUNTIME_DIR}"
    write_expected_crontab
    SERVICE_ACTIVE=1
    if ensure_acme_cron; then fail "missing SysV enable utility was accepted"; fi
    assert_file_lacks ' start' "${EVENT_LOG}"
done

reset_case yum-fallback package-install
release=centos
unset -f dnf
if ensure_acme_cron; then fail "missing chkconfig was accepted in the yum fallback"; fi
assert_file_contains 'pkg:yum:install -y cronie' "${EVENT_LOG}"

reset_case no-init no-init
rmdir "${NOVAS_SYSTEMD_RUNTIME_DIR}"
unset -f systemctl service rc-service rc-update
if ensure_acme_cron; then fail "cron setup succeeded without an init system"; fi
assert_eq "$(count_events 'pkg:')" 0 "no-init failure invoked a package manager"

unset -f apt-get dnf yum pacman crontab
for forbidden in systemctl service rc-service rc-update update-rc.d chkconfig crontab apt apt-get dnf yum pacman curl wget bash sh unknown-command; do
    if command -v "${forbidden}" >/dev/null 2>&1; then fail "host command reachable: ${forbidden}"; fi
    status=0
    command "${forbidden}" >/dev/null 2>&1 || status=$?
    assert_eq "${status}" 127 "unknown command was not denied"
done
assert_eq "${PATH}" "${utility_path}" "sandbox PATH escaped the allowlist"

printf 'Cloudflare shell-menu tests passed\n'
