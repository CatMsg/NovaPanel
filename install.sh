#!/bin/bash

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'

cur_dir=$(pwd)
AUTO_UPGRADE="${NOVAS_AUTO_UPGRADE:-0}"

github_api_url="https://api.github.com/repos/CatMsg/NovaPanel/releases/latest"

arch() {
    case "$(uname -m)" in
    x86_64 | x64 | amd64) echo 'amd64' ;;
    i*86 | x86) echo '386' ;;
    armv8* | armv8 | arm64 | aarch64) echo 'arm64' ;;
    armv7* | armv7 | arm) echo 'armv7' ;;
    armv6* | armv6) echo 'armv6' ;;
    armv5* | armv5) echo 'armv5' ;;
    s390x) echo 's390x' ;;
    *) echo -e "${green}不支持的 CPU 架构！${plain}" && rm -f install.sh && exit 1 ;;
    esac
}

download_to_file() {
    local url="$1"
    local dest="$2"

    if command -v curl >/dev/null 2>&1; then
        curl -fsSL --retry 3 --retry-delay 2 -o "$dest" "$url"
        return $?
    fi

    wget -q --show-progress -O "$dest" "$url"
}

verify_archive_checksum() {
    local version="$1"
    local archive_path="$2"
    local checksum_path="${archive_path}.sha256"
    local checksum_url="https://github.com/CatMsg/NovaPanel/releases/download/${version}/NovaPanel-linux-$(arch).tar.gz.sha256"

    if ! download_to_file "${checksum_url}" "${checksum_path}"; then
        rm -f "${checksum_path}"
        echo -e "${red}无法获取 SHA-256 校验文件，已停止安装。${plain}"
        return 1
    fi

    local expected actual
    expected=$(awk 'NF {print $1; exit}' "${checksum_path}")
    actual=$(sha256sum "${archive_path}" | awk '{print $1}')
    rm -f "${checksum_path}"
    if [[ -z "${expected}" || "${expected}" != "${actual}" ]]; then
        echo -e "${red}NovaPanel 下载包 SHA-256 校验失败，已停止安装。${plain}"
        return 1
    fi
    echo -e "${green}NovaPanel 下载包 SHA-256 校验通过。${plain}"
    return 0
}

get_latest_release_tag() {
    local response
    if command -v curl >/dev/null 2>&1; then
        response=$(curl -fsSL --retry 3 --retry-delay 2 "$github_api_url") || return 1
    else
        response=$(wget -qO- "$github_api_url") || return 1
    fi

    printf '%s\n' "$response" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/' | head -n1
}

install_base() {
    case "${release}" in
    centos | almalinux | rocky | oracle)
        yum -y update && yum install -y -q wget curl tar tzdata util-linux
        ;;
    fedora)
        dnf -y update && dnf install -y -q wget curl tar tzdata util-linux
        ;;
    arch | manjaro | parch)
        pacman -Syu && pacman -Syu --noconfirm wget curl tar tzdata util-linux
        ;;
    opensuse-tumbleweed)
        zypper refresh && zypper -q install -y wget curl tar timezone util-linux
        ;;
    *)
        apt-get update && apt-get install -y -q wget curl tar tzdata util-linux
        ;;
    esac
}

install_firewall_backend() {
    if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q '^Status: active'; then
        echo -e "${green}已检测到可用的 ufw。${plain}"
        return 0
    fi

    if command -v nft >/dev/null 2>&1 || command -v iptables >/dev/null 2>&1 || command -v ip6tables >/dev/null 2>&1; then
        echo -e "${green}已检测到可用端口转发组件，跳过安装。${plain}"
        return 0
    fi

    echo -e "${yellow}未检测到 ufw / iptables / nftables，正在尝试安装 iptables...${plain}"
    case "${release}" in
    centos | almalinux | rocky | oracle)
        yum install -y -q iptables || yum install -y -q nftables
        ;;
    fedora)
        dnf install -y -q iptables || dnf install -y -q nftables
        ;;
    arch | manjaro | parch)
        pacman -S --noconfirm --needed iptables || pacman -S --noconfirm --needed nftables
        ;;
    opensuse-tumbleweed)
        zypper -q install -y iptables || zypper -q install -y nftables
        ;;
    *)
        apt-get install -y -q iptables || apt-get install -y -q nftables
        ;;
    esac

    if command -v nft >/dev/null 2>&1 || command -v iptables >/dev/null 2>&1 || command -v ip6tables >/dev/null 2>&1; then
        echo -e "${green}端口转发组件安装完成。${plain}"
        return 0
    fi

    echo -e "${red}未能安装可用的端口转发组件，请手动安装 nftables 或 iptables。${plain}"
    exit 1
}

installer_is_fresh() {
    local root="${1:-}" path
    [[ -z "${NOVAS_DB_FOLDER:-}" ]] || return 1
    # A missing DB alone is not proof of a fresh install (including dangling links).
    for path in /usr/local/novas /usr/bin/novas /usr/local/bin/novas \
        /etc/systemd/system/novas.service /etc/systemd/system/novas.service.d \
        /usr/lib/systemd/system/novas.service /lib/systemd/system/novas.service \
        /var/lib/novas; do
        [[ ! -e "$root$path" && ! -L "$root$path" ]] || return 1
    done
    ! command -v novas >/dev/null 2>&1
}

installer_port_valid() {
    [[ "$1" =~ ^[0-9]{1,5}$ ]] && ((10#$1 > 0 && 10#$1 <= 65535))
}

installer_install_package() {
    local package="$1"
    case "$release" in
        debian | ubuntu | linuxmint | raspbian | armbian) apt-get install -y -q "$package" ;;
        fedora)
            [[ "$package" != iproute2 ]] || package=iproute
            dnf install -y -q "$package" ;;
        centos | almalinux | rocky | oracle)
            [[ "$package" != iproute2 ]] || package=iproute
            # Use configured distribution/EPEL repositories only; never add a repository.
            if command -v dnf >/dev/null 2>&1; then
                dnf install -y -q "$package"
            else
                yum install -y -q "$package"
            fi ;;
        arch | manjaro | parch) pacman -S --noconfirm --needed "$package" ;;
        opensuse-tumbleweed) zypper -q install -y "$package" ;;
        *) echo "Automatic $package installation is unsupported on $release" >&2; return 1 ;;
    esac || { echo "Failed to install $package from configured repositories on $release; install it explicitly and retry" >&2; return 1; }
}

installer_ssh_ports() {
    local config sockets port endpoint line client client_port server server_port extra unit binding listeners index
    local ports=() live=() configured=() socket_ports=() fields=()
    config=$(LC_ALL=C sshd -T) || { echo "Cannot read effective sshd configuration; refusing UFW activation" >&2; return 1; }
    sockets=$(LC_ALL=C ss -H -ltnp) || { echo "Cannot inspect live SSH listeners; refusing UFW activation" >&2; return 1; }
    if [[ "$sockets" == *'"systemd",pid=1,'* ]] && command -v systemctl >/dev/null 2>&1; then
        for unit in ssh.socket sshd.socket; do
            LC_ALL=C systemctl is-active --quiet "$unit" || continue
            binding=$(LC_ALL=C systemctl show "$unit" -p Triggers --value) || return 1
            case "$binding" in
                ssh.service | sshd.service | ssh@.service | sshd@.service) ;;
                *) echo "Unrecognized SSH socket service binding" >&2; return 1 ;;
            esac
            listeners=$(LC_ALL=C systemctl show "$unit" -p Listen --value) || return 1
            read -r -a fields <<< "$listeners"
            [[ ${#fields[@]} -gt 0 && $((${#fields[@]} % 2)) == 0 ]] || return 1
            for ((index=0; index<${#fields[@]}; index+=2)); do
                [[ "${fields[index+1]}" == '(Stream)' ]] || return 1
                endpoint="${fields[index]}"
                [[ "$endpoint" != /* && "$endpoint" != @* ]] || return 1
                port="${endpoint##*:}"
                installer_port_valid "$port" || return 1
                socket_ports+=("$((10#$port))")
            done
        done
    fi
    while read -r line; do
        endpoint=$(printf '%s\n' "$line" | awk '{print $4}')
        port="${endpoint##*:}"
        if [[ "$line" != *'"sshd"'* && "$line" != *'"sshd-session"'* ]]; then
            [[ "$line" == *'"systemd",pid=1,'* && " ${socket_ports[*]:-} " == *" $port "* ]] || continue
        fi
        installer_port_valid "$port" || return 1
        live+=("$((10#$port))")
    done <<< "$sockets"
    for port in ${socket_ports[@]+"${socket_ports[@]}"}; do
        [[ " ${live[*]:-} " == *" $port "* ]] || { echo "SSH socket configuration differs from live listeners" >&2; return 1; }
    done
    [[ ${#live[@]} -gt 0 ]] || { echo "No identifiable live sshd listener; refusing UFW activation" >&2; return 1; }
    while read -r line endpoint extra; do
        case "$line" in
            port) port="$endpoint" ;;
            listenaddress) port="${endpoint##*:}" ;;
            *) continue ;;
        esac
        installer_port_valid "$port" || return 1
        configured+=("$((10#$port))")
    done <<< "$config"
    [[ ${#configured[@]} -gt 0 ]] || { echo "No SSH ports in sshd configuration" >&2; return 1; }
    # Refuse stale/default config when the running daemon uses unknown overrides.
    for port in "${configured[@]}"; do
        # Active SSH sockets override sshd's configured ports; do not open unused defaults.
        [[ ${#socket_ports[@]} == 0 ]] || continue
        [[ " ${live[*]} " == *" $port "* ]] || { echo "sshd configuration differs from live listeners; refusing UFW activation" >&2; return 1; }
    done
    if [[ -n "${SSH_CONNECTION:-}" ]]; then
        read -r client client_port server server_port extra <<< "$SSH_CONNECTION"
        [[ -n "$client" && -n "$server" && -z "$extra" ]] && \
            installer_port_valid "$client_port" && installer_port_valid "$server_port" || {
            echo "Invalid SSH_CONNECTION; refusing UFW activation" >&2; return 1;
        }
        server_port="$((10#$server_port))"
        [[ " ${live[*]} " == *" $server_port "* ]] || { echo "Current SSH server port is not an identifiable listener" >&2; return 1; }
        ports+=("$server_port")
    fi
    printf '%s\n' ${ports[@]+"${ports[@]}"} "${live[@]}" | sed '/^$/d' | sort -nu
}

install_fresh_ufw() (
    local home="$1" root="${2:-}" ssh_ports settings panel_port sub_port port status ipv6_config
    local ports=()
    local ufw_was_active=0 activation_attempted=0 ufw_committed=0
    if ! command -v ss >/dev/null 2>&1; then
        installer_install_package iproute2 || return 1
    fi
    command -v ss >/dev/null 2>&1 || { echo "SSH listener inspection requires ss" >&2; return 1; }
    ssh_ports=$(installer_ssh_ports) || return 1
    settings=$(cd "$home" && NOVAS_DB_FOLDER="$home/db" ./novas setting -show) || return 1
    panel_port=$(printf '%s\n' "$settings" | awk '/^[[:space:]]*Panel port:/ {print $3}')
    sub_port=$(printf '%s\n' "$settings" | awk '/^[[:space:]]*Sub port:/ {print $3}')
    # Interactive setting changes synchronize forwarding immediately, so UFW
    # must already be active with the requested ports before invoking that CLI.
    panel_port="${3:-$panel_port}"
    sub_port="${4:-$sub_port}"
    installer_port_valid "$panel_port" && installer_port_valid "$sub_port" || {
        echo "Cannot safely determine panel/subscription ports; refusing UFW activation" >&2; return 1;
    }
    if ! command -v ufw >/dev/null 2>&1; then
        echo "Fresh installation requires UFW; installing ufw (no iptables fallback)."
        installer_install_package ufw || return 1
    fi
    command -v ufw >/dev/null 2>&1 || { echo "UFW installation failed" >&2; return 1; }
    status=$(LC_ALL=C ufw status) || return 1
    case "$status" in
        'Status: active'*) ufw_was_active=1 ;;
        'Status: inactive'*) ;;
        *) echo "Cannot snapshot UFW activation state" >&2; return 1 ;;
    esac
    installer_ufw_recover() {
        local rc=$?
        trap - EXIT INT TERM
        if [[ "$ufw_committed" == 0 && "$activation_attempted" == 1 ]]; then
            if [[ "$ufw_was_active" == 0 ]]; then
                # Restore only our activation, never disable a previously active firewall.
                LC_ALL=C ufw --force disable || echo "CRITICAL: UFW recovery failed; check firewall access from console" >&2
            else
                echo "UFW setup failed; previously active UFW and added safety rules are retained" >&2
            fi
        fi
        exit "$rc"
    }
    trap installer_ufw_recover EXIT
    trap 'exit 130' INT
    trap 'exit 143' TERM
    ipv6_config="$root/etc/default/ufw"
    [[ -f "$ipv6_config" && ! -L "$ipv6_config" ]] || { echo "Missing/linked UFW IPv6 configuration" >&2; return 1; }
    # Preserve other UFW settings and all existing rules; generic allows cover both families.
    if [[ $(grep -Ec '^[[:space:]]*IPV6[[:space:]]*=' "$ipv6_config") != 1 ]] || ! grep -Eq '^IPV6=yes$' "$ipv6_config"; then
        sed -i.bak '/^[[:space:]]*IPV6[[:space:]]*=/d' "$ipv6_config" || return 1
        printf '\nIPV6=yes\n' >> "$ipv6_config" || return 1
    fi
    while read -r port; do ports+=("$port"); done <<< "$ssh_ports"
    ports+=("$((10#$panel_port))" "$((10#$sub_port))")
    for port in "${ports[@]}"; do
        # Prepend so existing deny rules cannot shadow SSH/panel access.
        LC_ALL=C ufw prepend allow "$port/tcp" || return 1
    done
    # UFW can skip an existing duplicate instead of moving it. Check the saved
    # rule order before activation; conservatively reject shadowed/unknown rules.
    local added
    added=$(LC_ALL=C ufw show added) || return 1
    for port in "${ports[@]}"; do
        if ! printf '%s\n' "$added" | awk -v p="$port/tcp" '
            $1 == "ufw" && $2 != "allow" {blocked=1}
            $1 == "ufw" && $2 == "allow" && $3 == p && ($4 == "" || $4 == "comment") && !blocked {found=1}
            END {exit !found}'; then
            echo "Cannot confirm unshadowed UFW access for $port/tcp before activation" >&2
            return 1
        fi
    done
    # Package installation may take time: refuse newly changed SSH listeners.
    ssh_ports=$(installer_ssh_ports) || return 1
    while read -r port; do
        [[ " ${ports[*]} " == *" $port "* ]] || { echo "SSH listeners changed before UFW activation" >&2; return 1; }
    done <<< "$ssh_ports"
    activation_attempted=1
    LC_ALL=C ufw --force enable || return 1
    LC_ALL=C ufw reload || return 1
    status=$(LC_ALL=C ufw status) || return 1
    [[ "$status" == *'Status: active'* ]] || { echo "UFW did not become active" >&2; return 1; }
    for port in "${ports[@]}"; do
        if ! printf '%s\n' "$status" | awk -v p="$port/tcp" '
            $1 == p && $2 == "ALLOW" && /Anywhere/ {v4=1}
            $1 == p && $2 == "(v6)" && $3 == "ALLOW" && /Anywhere \(v6\)/ {v6=1}
            END {exit !(v4 && v6)}'; then
            echo "UFW is missing IPv4/IPv6 access for $port/tcp" >&2
            return 1
        fi
    done
    if [[ "$ufw_was_active" == 0 && -n "${UFW_RECOVERY_STATE:-}" ]]; then
        # The deployment's EXIT trap must still undo activation if panel startup fails.
        printf 'inactive\n' > "$UFW_RECOVERY_STATE" || return 1
    fi
    ufw_committed=1
)

install_login_protection() {
    if [[ ! -d /run/systemd/system ]] || ! command -v systemctl >/dev/null 2>&1; then
        echo -e "${yellow}当前环境未运行 systemd，跳过 Fail2ban 登录防护安装。${plain}"
        return 0
    fi
    if command -v fail2ban-client >/dev/null 2>&1; then
        echo -e "${green}已检测到 Fail2ban 登录防护组件。${plain}"
        return 0
    fi

    echo -e "${yellow}正在安装 Fail2ban 登录防护组件...${plain}"
    local installed=0
    case "${release}" in
    centos | almalinux | rocky | oracle)
        yum install -y -q fail2ban && installed=1
        ;;
    fedora)
        dnf install -y -q fail2ban && installed=1
        ;;
    arch | manjaro | parch)
        pacman -S --noconfirm --needed fail2ban && installed=1
        ;;
    opensuse-tumbleweed)
        zypper -q install -y fail2ban && installed=1
        ;;
    *)
        apt-get install -y -q fail2ban && installed=1
        ;;
    esac
    if [[ "$installed" == "1" ]] && command -v fail2ban-client >/dev/null 2>&1; then
        echo -e "${green}Fail2ban 安装完成，NovaPanel 启动后将自动加载登录防护。${plain}"
        return 0
    fi
    echo -e "${yellow}Fail2ban 自动安装失败；面板会继续安装，请在健康诊断中查看并手动处理。${plain}"
    return 0
}

config_after_install() {
    local home="${1:-/usr/local/novas}" root="${2:-}"

    if [[ "${AUTO_UPGRADE}" == "1" ]]; then
        if [[ ! -f "$home/db/novas.db" ]]; then
            local usernameTemp=$(head -c 6 /dev/urandom | base64)
            local passwordTemp=$(head -c 6 /dev/urandom | base64)
            echo -e "检测到自动安装模式，已生成随机登录信息："
            echo -e "###############################################"
            echo -e "${green}用户名：${usernameTemp}${plain}"
            echo -e "${green}密码：${passwordTemp}${plain}"
            echo -e "###############################################"
            "$home/novas" admin -username "${usernameTemp}" -password "${passwordTemp}" || return 1
        else
            echo -e "${green}检测到自动升级模式，已保留现有配置并跳过交互提示。${plain}"
        fi
        return 0
    fi

    echo -e "${yellow}安装/更新完成！出于安全考虑，建议修改面板设置 ${plain}"
    read -p "是否继续修改设置 [y/n]？": config_confirm
    if [[ "${config_confirm}" == "y" || "${config_confirm}" == "Y" ]]; then
        echo -e "请输入${yellow}面板端口${plain}（留空则使用现有/默认值）："
        read config_port
        echo -e "请输入${yellow}面板路径${plain}（留空则使用现有/默认值）："
        read config_path

        # 订阅配置
        echo -e "请输入${yellow}订阅端口${plain}（留空则使用现有/默认值）："
        read config_subPort
        echo -e "请输入${yellow}订阅路径${plain}（留空则使用现有/默认值）："
        read config_subPath

        # 设置配置
        echo -e "${yellow}正在初始化，请稍候...${plain}"
        local params=()
        [ -z "$config_port" ] || params+=(-port "$config_port")
        [ -z "$config_path" ] || params+=(-path "$config_path")
        [ -z "$config_subPort" ] || params+=(-subPort "$config_subPort")
        [ -z "$config_subPath" ] || params+=(-subPath "$config_subPath")
        if [[ "${FRESH_INSTALL:-0}" == 1 ]]; then
            install_fresh_ufw "$home" "$root" "$config_port" "$config_subPort" || return 1
        fi
        if ! "$home/novas" setting ${params[@]+"${params[@]}"}; then
            echo -e "${red}面板设置初始化失败，正在回滚安装。${plain}"
            return 1
        fi

        read -p "是否修改管理员账号密码 [y/n]？": admin_confirm
        if [[ "${admin_confirm}" == "y" || "${admin_confirm}" == "Y" ]]; then
            # 首个管理员账号密码
            read -p "请设置用户名：" config_account
            read -p "请设置密码：" config_password

            # 设置账号密码
            echo -e "${yellow}正在初始化，请稍候...${plain}"
            "$home/novas" admin -username "${config_account}" -password "${config_password}" || return 1
        else
            echo -e "${yellow}当前管理员账号密码：${plain}"
            "$home/novas" admin -show || return 1
        fi
    else
        echo -e "${red}已取消...${plain}"
        if [[ ! -f "$home/db/novas.db" ]]; then
            local usernameTemp=$(head -c 6 /dev/urandom | base64)
            local passwordTemp=$(head -c 6 /dev/urandom | base64)
            echo -e "这是全新安装，出于安全考虑将生成随机登录信息："
            echo -e "###############################################"
            echo -e "${green}用户名：${usernameTemp}${plain}"
            echo -e "${green}密码：${passwordTemp}${plain}"
            echo -e "###############################################"
            echo -e "${red}如果忘记登录信息，可以输入 ${green}novas${red}打开配置菜单${plain}"
            "$home/novas" admin -username "${usernameTemp}" -password "${passwordTemp}" || return 1
        else
            echo -e "${red}这是升级安装，将保留旧设置；如果忘记登录信息，可以输入 ${green}novas${red}打开配置菜单${plain}"
        fi
    fi
}

novas_configure() {
    if [[ -n "$2" ]]; then
        echo "检测到现有安装，保留管理员、证书、端口和订阅配置，不执行重置。"
        return 0
    fi
    config_after_install "$1" || return 1
    if [[ "${FRESH_INSTALL:-0}" == 1 ]]; then
        install_fresh_ufw "$1" || { echo "Fresh-install UFW setup failed; installation stopped (no backend fallback)." >&2; return 1; }
    fi
}

installer_deployment_cleanup() {
    local staging="$1" rc="$2"
    trap - EXIT
    if [[ -f "$staging/ufw-new-activation" ]]; then
        LC_ALL=C ufw --force disable || echo "CRITICAL: UFW recovery failed; check firewall access from console" >&2
    fi
    rm -rf "$staging"
    exit "$rc"
}

install_novas() {
    local version="${1:-}" staging archive
    if [[ -z "$version" ]]; then version=$(get_latest_release_tag) || return 1; fi
    [[ -n "$version" ]] || { echo "无法获取发布版本" >&2; return 1; }
    [[ "$version" == v* ]] || version="v$version"
    [[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "无效版本号" >&2; return 1; }
    staging=$(mktemp -d /tmp/novapanel-install.XXXXXX) || return 1
    local UFW_RECOVERY_STATE="$staging/ufw-new-activation"
    trap "installer_deployment_cleanup '$staging' \$?" EXIT
    archive="$staging/NovaPanel-linux-$(arch).tar.gz"
    download_to_file "https://github.com/CatMsg/NovaPanel/releases/download/$version/NovaPanel-linux-$(arch).tar.gz" "$archive" || return 1
    verify_archive_checksum "$version" "$archive" || return 1
    # Reject path traversal before extracting a downloaded archive.
    if tar -tzf "$archive" | grep -E '(^/|(^|/)\.\.(/|$))' >/dev/null; then
        echo "安装包包含非法路径" >&2
        return 1
    fi
    tar -xzf "$archive" -C "$staging" || return 1
    [[ -f "$staging/novas/scripts/install-runtime.sh" && -x "$staging/novas/novas" ]] || {
        echo "需要新的 novas 格式发布包；未改动当前安装。旧版本降级请使用对应 tag 的安装脚本。" >&2
        return 1
    }
    source "$staging/novas/scripts/install-runtime.sh"
    novas_deploy "$staging/novas"
    rm -f "$UFW_RECOVERY_STATE"
    echo "NovaPanel $version 安装成功。使用 novas 管理面板。"
    /usr/local/novas/novas uri
    rm -rf "$staging"
    trap - EXIT
}

install_main() {
    [[ $EUID -eq 0 ]] || { echo "请使用 root 权限运行此脚本" >&2; return 1; }
    if [[ -f /etc/os-release ]]; then
        source /etc/os-release
    elif [[ -f /usr/lib/os-release ]]; then
        source /usr/lib/os-release
    else
        echo "检测系统失败，请联系作者！" >&2
        return 1
    fi
    release=$ID
    echo "当前系统发行版为：$release"
    echo "架构：$(arch)"
    FRESH_INSTALL=0
    installer_is_fresh && FRESH_INSTALL=1
    echo -e "${green}正在执行...${plain}"
    install_base
    # Upgrades retain the original backend preflight; fresh installs use the
    # locked deployment callback, before port forwarding or service startup.
    if [[ "$FRESH_INSTALL" == 0 ]]; then install_firewall_backend; fi
    install_login_protection
    install_novas "${1:-}"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    set -e
    install_main "${1:-}"
fi
