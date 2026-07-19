#!/bin/bash

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'

cur_dir=$(pwd)
AUTO_UPGRADE="${SUI_AUTO_UPGRADE:-0}"

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
        echo -e "${yellow}当前发布未提供 SHA-256 校验文件，跳过完整性校验。${plain}"
        return 0
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
        yum -y update && yum install -y -q wget curl tar tzdata
        ;;
    fedora)
        dnf -y update && dnf install -y -q wget curl tar tzdata
        ;;
    arch | manjaro | parch)
        pacman -Syu && pacman -Syu --noconfirm wget curl tar tzdata
        ;;
    opensuse-tumbleweed)
        zypper refresh && zypper -q install -y wget curl tar timezone
        ;;
    *)
        apt-get update && apt-get install -y -q wget curl tar tzdata
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

config_after_install() {
    echo -e "${yellow}正在迁移... ${plain}"
    /usr/local/s-ui/sui migrate

    if [[ "${AUTO_UPGRADE}" == "1" ]]; then
        if [[ ! -f "/usr/local/s-ui/db/s-ui.db" ]]; then
            local usernameTemp=$(head -c 6 /dev/urandom | base64)
            local passwordTemp=$(head -c 6 /dev/urandom | base64)
            echo -e "检测到自动安装模式，已生成随机登录信息："
            echo -e "###############################################"
            echo -e "${green}用户名：${usernameTemp}${plain}"
            echo -e "${green}密码：${passwordTemp}${plain}"
            echo -e "###############################################"
            /usr/local/s-ui/sui admin -username ${usernameTemp} -password ${passwordTemp}
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
        params=""
        [ -z "$config_port" ] || params="$params -port $config_port"
        [ -z "$config_path" ] || params="$params -path $config_path"
        [ -z "$config_subPort" ] || params="$params -subPort $config_subPort"
        [ -z "$config_subPath" ] || params="$params -subPath $config_subPath"
        /usr/local/s-ui/sui setting ${params}

        read -p "是否修改管理员账号密码 [y/n]？": admin_confirm
        if [[ "${admin_confirm}" == "y" || "${admin_confirm}" == "Y" ]]; then
            # 首个管理员账号密码
            read -p "请设置用户名：" config_account
            read -p "请设置密码：" config_password

            # 设置账号密码
            echo -e "${yellow}正在初始化，请稍候...${plain}"
            /usr/local/s-ui/sui admin -username ${config_account} -password ${config_password}
        else
            echo -e "${yellow}当前管理员账号密码：${plain}"
            /usr/local/s-ui/sui admin -show
        fi
    else
        echo -e "${red}已取消...${plain}"
        if [[ ! -f "/usr/local/s-ui/db/s-ui.db" ]]; then
            local usernameTemp=$(head -c 6 /dev/urandom | base64)
            local passwordTemp=$(head -c 6 /dev/urandom | base64)
            echo -e "这是全新安装，出于安全考虑将生成随机登录信息："
            echo -e "###############################################"
            echo -e "${green}用户名：${usernameTemp}${plain}"
            echo -e "${green}密码：${passwordTemp}${plain}"
            echo -e "###############################################"
            echo -e "${red}如果忘记登录信息，可以输入 ${green}s-ui${red}（兼容命令）打开配置菜单${plain}"
            /usr/local/s-ui/sui admin -username ${usernameTemp} -password ${passwordTemp}
        else
            echo -e "${red}这是升级安装，将保留旧设置；如果忘记登录信息，可以输入 ${green}s-ui${red}（兼容命令）打开配置菜单${plain}"
        fi
    fi
}

prepare_services() {
    if [[ -f "/etc/systemd/system/sing-box.service" ]]; then
        echo -e "${yellow}正在停止 sing-box 服务... ${plain}"
        systemctl stop sing-box
        rm -f /usr/local/s-ui/bin/sing-box /usr/local/s-ui/bin/runSingbox.sh /usr/local/s-ui/bin/signal
    fi
    if [[ -e "/usr/local/s-ui/bin" ]]; then
        echo -e "###############################################################"
        echo -e "${green}/usr/local/s-ui/bin${red} 目录已存在！"
        echo -e "请检查其中内容，并在迁移后手动删除 ${plain}"
        echo -e "###############################################################"
    fi
    systemctl daemon-reload
}

save_install_rollback() {
    local backup_dir="$1"
    if [[ ! -d "/usr/local/s-ui" ]]; then
        return 1
    fi
    rm -rf "${backup_dir}"
    mkdir -p "${backup_dir}" || return 1
    [[ -f "/usr/local/s-ui/sui" ]] && cp -a "/usr/local/s-ui/sui" "${backup_dir}/sui" || true
    [[ -f "/usr/local/s-ui/s-ui.sh" ]] && cp -a "/usr/local/s-ui/s-ui.sh" "${backup_dir}/s-ui.sh" || true
    [[ -d "/usr/local/s-ui/scripts" ]] && cp -a "/usr/local/s-ui/scripts" "${backup_dir}/scripts" || true
    [[ -f "/etc/systemd/system/s-ui.service" ]] && cp -a "/etc/systemd/system/s-ui.service" "${backup_dir}/s-ui.service" || true
    [[ -f "${backup_dir}/sui" || -f "${backup_dir}/s-ui.sh" ]]
}

restore_install_rollback() {
    local backup_dir="$1"
    LOGE "安装失败，正在回滚上一版核心文件..."
    systemctl stop s-ui 2>/dev/null || true
    [[ -f "${backup_dir}/sui" ]] && cp -f "${backup_dir}/sui" "/usr/local/s-ui/sui"
    [[ -f "${backup_dir}/s-ui.sh" ]] && cp -f "${backup_dir}/s-ui.sh" "/usr/local/s-ui/s-ui.sh"
    [[ -d "${backup_dir}/scripts" ]] && cp -rf "${backup_dir}/scripts" "/usr/local/s-ui/"
    [[ -f "${backup_dir}/s-ui.service" ]] && cp -f "${backup_dir}/s-ui.service" "/etc/systemd/system/s-ui.service"
    chmod +x "/usr/local/s-ui/sui" "/usr/local/s-ui/s-ui.sh" 2>/dev/null || true
    systemctl daemon-reload 2>/dev/null || true
    systemctl enable s-ui --now 2>/dev/null || true
}

install_s-ui() {
    cd /tmp/
    local archive_path="/tmp/NovaPanel-linux-$(arch).tar.gz"
    local staging_dir=""
    local rollback_dir="/var/lib/s-ui/update-backup/$(date +%Y%m%d-%H%M%S)"

    if [ $# == 0 ]; then
        last_version=$(get_latest_release_tag)
        if [[ ! -n "$last_version" ]]; then
            echo -e "${red}获取 NovaPanel 版本失败，可能是 Github API 限制导致，请稍后重试${plain}"
            exit 1
        fi
        echo -e "已获取 NovaPanel 最新版本：${last_version}，开始安装..."
        download_to_file "https://github.com/CatMsg/NovaPanel/releases/download/${last_version}/NovaPanel-linux-$(arch).tar.gz" "${archive_path}"
        if [[ $? -ne 0 ]]; then
            echo -e "${red}下载 NovaPanel 失败，请确认服务器可以访问 Github ${plain}"
            exit 1
        fi
    else
        last_version=$1
        [[ "${last_version}" != v* ]] && last_version="v${last_version}"
        url="https://github.com/CatMsg/NovaPanel/releases/download/${last_version}/NovaPanel-linux-$(arch).tar.gz"
        echo -e "开始安装 NovaPanel ${last_version}"
        download_to_file "${url}" "${archive_path}"
        if [[ $? -ne 0 ]]; then
            echo -e "${red}下载 NovaPanel ${last_version} 失败，请检查该版本是否存在${plain}"
            exit 1
        fi
    fi

    if ! verify_archive_checksum "${last_version}" "${archive_path}"; then
        rm -f "${archive_path}"
        exit 1
    fi

    if [[ -d /usr/local/s-ui/ ]]; then
        if ! save_install_rollback "${rollback_dir}"; then
            echo -e "${red}无法创建安装回滚副本，已停止更新。${plain}"
            rm -f "${archive_path}"
            exit 1
        fi
        systemctl stop s-ui
    fi

    staging_dir=$(mktemp -d /tmp/novapanel-install.XXXXXX)
    if ! tar zxf "${archive_path}" -C "${staging_dir}" || [[ ! -x "${staging_dir}/s-ui/sui" ]]; then
        echo -e "${red}NovaPanel 安装包解压失败。${plain}"
        [[ -n "${rollback_dir}" && -d "${rollback_dir}" ]] && restore_install_rollback "${rollback_dir}"
        rm -rf "${staging_dir}" "${archive_path}"
        exit 1
    fi
    rm -f "${archive_path}"

    chmod +x "${staging_dir}/s-ui/sui" "${staging_dir}/s-ui/s-ui.sh"
    if ! cp -f "${staging_dir}/s-ui/s-ui.sh" /usr/bin/s-ui || \
       ! cp -rf "${staging_dir}/s-ui" /usr/local/ || \
       ! cp -f "${staging_dir}/s-ui"/*.service /etc/systemd/system/; then
        [[ -d "${rollback_dir}" ]] && restore_install_rollback "${rollback_dir}"
        rm -rf "${staging_dir}"
        exit 1
    fi
    rm -rf "${staging_dir}"

    if ! config_after_install || ! prepare_services; then
        [[ -d "${rollback_dir}" ]] && restore_install_rollback "${rollback_dir}"
        exit 1
    fi

    if ! systemctl enable s-ui --now; then
        [[ -d "${rollback_dir}" ]] && restore_install_rollback "${rollback_dir}"
        exit 1
    fi

    if [[ -d "/var/lib/s-ui/update-backup" ]]; then
        find /var/lib/s-ui/update-backup -mindepth 1 -maxdepth 1 -type d -printf '%T@ %p\n' 2>/dev/null | sort -nr | tail -n +4 | cut -d' ' -f2- | xargs -r rm -rf
    fi

    echo -e "${green}NovaPanel ${last_version}${plain} 安装完成，现已启动并运行..."
    echo -e "你可以通过以下 URL 访问面板：${green}"
    /usr/local/s-ui/sui uri
    echo -e "${plain}"
    echo -e ""
    echo -e "如需管理面板，可执行 ${green}s-ui help${plain}（兼容命令）。"
    s-ui help
}

echo -e "${green}正在执行...${plain}"
install_base
install_firewall_backend
install_s-ui $1
