#!/bin/bash
# Sourced by install.sh after the release archive has passed its SHA-256 check.
# The optional filesystem root is used only by isolated installer tests.
novas_deploy() (
    set -Eeuo pipefail
    umask 077
    # These belong to this subshell, not function-local scope: EXIT traps must
    # still be able to read them when errexit unwinds the function.
    package="$1" root="${2:-}"
    home="${root}/usr/local/novas"
    units="${root}/etc/systemd/system" commands="${root}/usr/bin"
    state="${root}/var/lib/novas"
    source_dir="" old_name="" snapshot="" stage=""
    stopped=0 deployed=0 committed=0 old_active=0 old_enabled=0

    for file in novas novas.sh novas.service scripts/hy2-forward.sh scripts/login-guard.sh bin/mita; do
        [[ -f "$package/$file" && ! -L "$package/$file" ]] || { echo "Incomplete novas archive: $file" >&2; exit 1; }
    done
    [[ -x "$package/novas" ]] || exit 1
    "$package/novas" -v >/dev/null
    mkdir -p "$commands" "$units" "${root}/var/lib" "${root}/usr/local"
    # This lock covers CLI, installer and API-triggered deployments alike.
    exec 9>"${root}/var/lib/novas-install.lock"
    flock -n 9 || { echo "Another NovaPanel installation is running" >&2; exit 1; }
    if [[ -d "$home" && ! -L "$home" ]]; then
        source_dir="$home"
        old_name=novas
    elif [[ -e "$home" || -L "$home" ]]; then
        echo "Unsupported installation path or dangling link" >&2
        exit 1
    fi
    # Refuse custom storage the standard installer cannot safely snapshot.
    if [[ -n "$old_name" ]]; then
        for linked in db bin scripts db/novas.db; do
            [[ ! -L "$source_dir/$linked" ]] || { echo "Linked storage/runtime is not supported by the system installer: $linked" >&2; exit 1; }
        done
        local unit_config
        unit_config=$(systemctl cat "$old_name.service")
        if printf '%s\n' "$unit_config" | grep -E '^[[:space:]]*(Environment(File)?=.*(DB_FOLDER|EnvironmentFile)|EnvironmentFile=|RootDirectory=|RootImage=)' >/dev/null; then
            echo "Custom database/environment/root override detected; use a custom installation procedure" >&2
            exit 1
        fi
        if systemctl show "$old_name.service" -p Environment --value | grep -E 'NOVAS_DB_FOLDER=' >/dev/null; then
            echo "Custom database folder detected; default-directory upgrade refused" >&2
            exit 1
        fi
        systemctl is-active --quiet "$old_name" && old_active=1
        systemctl is-enabled --quiet "$old_name" && old_enabled=1
    fi
    [[ -z "${NOVAS_DB_FOLDER:-}" ]] || { echo "Unset custom DB_FOLDER before using the system installer" >&2; exit 1; }

    novas_rollback() {
        local rc=$?
        trap - EXIT INT TERM
        if [[ "$committed" == 0 ]]; then
            echo "Installation failed; restoring the previous installation" >&2
            if [[ "$deployed" == 1 ]]; then
                systemctl stop novas || true
                systemctl disable novas || true
                rm -rf "$home" "$units/novas.service.d"
                rm -f "$units/novas.service" "$commands/novas"
                if [[ -n "$source_dir" && -d "$snapshot/install" ]]; then
                    mkdir -p "$source_dir"
                    cp -a "$snapshot/install/." "$source_dir/"
                    if [[ -f "$snapshot/service" ]]; then cp -a "$snapshot/service" "$units/$old_name.service"; fi
                    if [[ -d "$snapshot/dropins" ]]; then cp -a "$snapshot/dropins" "$units/$old_name.service.d"; fi
                    if [[ -f "$snapshot/command" ]]; then cp -a "$snapshot/command" "$commands/$old_name"; fi
                fi
                systemctl daemon-reload || true
            fi
            if [[ "$stopped" == 1 && -n "$old_name" ]]; then
                if [[ "$old_enabled" == 1 ]]; then systemctl enable "$old_name" || true; fi
                if [[ "$old_active" == 1 ]]; then systemctl start "$old_name" || true; fi
            fi
        fi
        [[ -z "$stage" ]] || rm -rf "$stage"
        exit "$rc"
    }
    trap novas_rollback EXIT
    trap 'exit 130' INT
    trap 'exit 143' TERM

    if [[ -n "$old_name" ]]; then
        systemctl stop "$old_name"
        stopped=1
        if systemctl is-active --quiet "$old_name"; then exit 1; fi
        # A complete offline copy includes the DB, WAL, certificates and assets.
        mkdir -p "$state/update-backup"
        snapshot=$(mktemp -d "$state/update-backup/$(date +%Y%m%d-%H%M%S).XXXXXX")
        cp -a "$source_dir" "$snapshot/install"
        [[ ! -f "$units/$old_name.service" ]] || cp -aL "$units/$old_name.service" "$snapshot/service"
        [[ ! -d "$units/$old_name.service.d" ]] || cp -a "$units/$old_name.service.d" "$snapshot/dropins"
        [[ ! -f "$commands/$old_name" ]] || cp -aL "$commands/$old_name" "$snapshot/command"
        printf '%s\n' "$old_name" > "$snapshot/identity"
    fi
    stage=$(mktemp -d "${root}/usr/local/.novas-stage.XXXXXX")
    if [[ -n "$source_dir" ]]; then cp -a "$source_dir/." "$stage/"; fi
    rm -f "$stage/novas" "$stage/novas.sh" "$stage/novas.service" "$stage/bin/mita" "$stage/scripts/hy2-forward.sh" "$stage/scripts/login-guard.sh" "$stage/scripts/install-runtime.sh"
    cp -a "$package/." "$stage/"
    chmod 755 "$stage"
    chmod 755 "$stage/novas" "$stage/novas.sh" "$stage/scripts/"*.sh
    # Work on the staged offline database, never on the only rollback copy.
    (cd "$stage"; env NOVAS_DB_FOLDER="$stage/db" ./novas checkdb)

    deployed=1
    rm -rf "$home"
    mv "$stage" "$home"
    stage=""
    install -m 755 "$home/novas.sh" "$commands/novas"
    install -m 644 "$home/novas.service" "$units/novas.service"
    if [[ -n "$snapshot" && -f "$snapshot/service" ]]; then
        # Preserve administrator limits and service options.
        cp -a "$snapshot/service" "$units/novas.service"
    fi
    if [[ -n "$snapshot" && -d "$snapshot/dropins" ]]; then
        mkdir -p "$units/novas.service.d"
        local file
        for file in "$snapshot/dropins/"*.conf; do
            [[ -f "$file" ]] || continue
            cp -a "$file" "$units/novas.service.d/$(basename "$file")"
        done
    fi
    systemctl daemon-reload
    novas_configure "$home" "$old_name"
    if [[ -z "$old_name" || "$old_enabled" == 1 ]]; then systemctl enable novas; fi
    systemctl start novas
    local healthy=0 attempt pid="" current_pid
    for ((attempt=0; attempt<30; attempt++)); do
        if systemctl is-active --quiet novas && "$home/novas" healthcheck; then
            current_pid=$(systemctl show novas -p MainPID --value)
            if [[ "$current_pid" == "$pid" && "$pid" != 0 && -n "$pid" ]]; then healthy=$((healthy+1)); else healthy=1; fi
            pid="$current_pid"
            [[ "$healthy" -lt 3 ]] || break
        else
            healthy=0
        fi
        sleep 2
    done
    [[ "$healthy" -ge 3 ]] || { echo "New panel failed the startup health check" >&2; exit 1; }
    committed=1
    # Keep three complete rollback snapshots; only touch installer-owned names.
    if [[ -d "$state/update-backup" ]]; then
        find "$state/update-backup" -mindepth 1 -maxdepth 1 -type d -name '????????-??????.*' -print | sort -r | tail -n +4 | while IFS= read -r expired; do rm -rf "$expired"; done
    fi
    echo "NovaPanel is now managed by: novas"
    [[ -z "$snapshot" ]] || echo "Offline rollback snapshot: $snapshot"
)
