#!/bin/bash

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'

cur_dir=$(pwd)
AUTO_UPGRADE="${NOVAS_AUTO_UPGRADE:-0}"

# 检查 root 权限
[[ $EUID -ne 0 ]] && echo -e "${red}致命错误：${plain}请使用 root 权限运行此脚本 \n " && exit 1

# 检查系统并设置 release 变量
if [[ -f /etc/os-release ]]; then
    source /etc/os-release
    release=$ID
elif [[ -f /usr/lib/os-release ]]; then
    source /usr/lib/os-release
    release=$ID
else
    echo "检测系统失败，请联系作者！" >&2
    exit 1
fi
echo "当前系统发行版为：$release"

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

echo "架构：$(arch)"

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

    if [[ "${AUTO_UPGRADE}" == "1" ]]; then
        if [[ ! -f "/usr/local/novas/db/novas.db" ]]; then
            local usernameTemp=$(head -c 6 /dev/urandom | base64)
            local passwordTemp=$(head -c 6 /dev/urandom | base64)
            echo -e "检测到自动安装模式，已生成随机登录信息："
            echo -e "###############################################"
            echo -e "${green}用户名：${usernameTemp}${plain}"
            echo -e "${green}密码：${passwordTemp}${plain}"
            echo -e "###############################################"
            /usr/local/novas/novas admin -username ${usernameTemp} -password ${passwordTemp}
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
        if ! /usr/local/novas/novas setting "${params[@]}"; then
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
            /usr/local/novas/novas admin -username "${config_account}" -password "${config_password}" || return 1
        else
            echo -e "${yellow}当前管理员账号密码：${plain}"
            /usr/local/novas/novas admin -show
        fi
    else
        echo -e "${red}已取消...${plain}"
        if [[ ! -f "/usr/local/novas/db/novas.db" ]]; then
            local usernameTemp=$(head -c 6 /dev/urandom | base64)
            local passwordTemp=$(head -c 6 /dev/urandom | base64)
            echo -e "这是全新安装，出于安全考虑将生成随机登录信息："
            echo -e "###############################################"
            echo -e "${green}用户名：${usernameTemp}${plain}"
            echo -e "${green}密码：${passwordTemp}${plain}"
            echo -e "###############################################"
            echo -e "${red}如果忘记登录信息，可以输入 ${green}novas${red}打开配置菜单${plain}"
            /usr/local/novas/novas admin -username ${usernameTemp} -password ${passwordTemp}
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
    config_after_install
}

install_novas() {
    local version="${1:-}" staging archive
    if [[ -z "$version" ]]; then version=$(get_latest_release_tag) || return 1; fi
    [[ -n "$version" ]] || { echo "无法获取发布版本" >&2; return 1; }
    [[ "$version" == v* ]] || version="v$version"
    [[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "无效版本号" >&2; return 1; }
    staging=$(mktemp -d /tmp/novapanel-install.XXXXXX) || return 1
    trap "rm -rf '$staging'" EXIT
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
    echo "NovaPanel $version 安装成功。使用 novas 管理面板。"
    /usr/local/novas/novas uri
    rm -rf "$staging"
    trap - EXIT
}

set -e
echo -e "${green}正在执行...${plain}"
install_base
install_firewall_backend
install_login_protection
install_novas "${1:-}"
