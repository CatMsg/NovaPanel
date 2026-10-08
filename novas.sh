#!/bin/bash

red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
plain='\033[0m'

function LOGD() {
    echo -e "${yellow}[调试] $* ${plain}"
}

function LOGE() {
    echo -e "${red}[错误] $* ${plain}"
}

function LOGI() {
    echo -e "${green}[信息] $* ${plain}"
}

[[ $EUID -ne 0 ]] && LOGE "错误：必须使用 root 权限运行此脚本！\n" && exit 1

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

readonly NOVAS_INSTALL_SCRIPT_URL="https://raw.githubusercontent.com/CatMsg/NovaPanel/main/install.sh"
readonly NOVAS_UPDATE_STATE_DIR="/var/lib/novas"
readonly NOVAS_UPDATE_LOG="${NOVAS_UPDATE_STATE_DIR}/update.log"
readonly NOVAS_UPDATE_STATUS="${NOVAS_UPDATE_STATE_DIR}/update.status"
readonly NOVAS_UPDATE_PID="${NOVAS_UPDATE_STATE_DIR}/update.pid"
readonly NOVAS_UPDATE_LOCK_DIR="${NOVAS_UPDATE_STATE_DIR}/update.lock"
readonly NOVAS_UPDATE_UNIT_FILE="${NOVAS_UPDATE_STATE_DIR}/update.unit"

confirm() {
    if [[ $# > 1 ]]; then
        echo && read -p "$1 [默认$2]: " temp
        if [[ x"${temp}" == x"" ]]; then
            temp=$2
        fi
    else
        read -p "$1 [y/n]： " temp
    fi
    if [[ x"${temp}" == x"y" || x"${temp}" == x"Y" ]]; then
        return 0
    else
        return 1
    fi
}

confirm_restart() {
    confirm "重启 ${1} 服务" "y"
    if [[ $? == 0 ]]; then
        restart
    else
        show_menu
    fi
}

before_show_menu() {
    echo && echo -n -e "${yellow}按回车返回主菜单：${plain}" && read temp
    show_menu
}

install() {
    run_install_script
    if [[ $? == 0 ]]; then
        if [[ $# == 0 ]]; then
            start
        else
            start 0
        fi
    fi
}

update() {
    current_version=""
    latest_version=""
    if [[ -x /usr/local/novas/novas ]]; then
        current_version=$(/usr/local/novas/novas -v 2>/dev/null | awk '/^NovaPanel Panel[[:space:]]+/ {print $NF}' | head -n1)
        current_version="v${current_version#v}"
    fi
    latest_version=$(curl -fsSL --max-time 30 "https://api.github.com/repos/CatMsg/NovaPanel/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
    if [[ -n "${current_version}" && -n "${latest_version}" && "${current_version}" == "${latest_version}" && "${NOVAS_AUTO_UPGRADE:-}" != "1" ]]; then
        confirm "当前版本 ${current_version} 与最新版本一致，是否仍然覆盖安装？" "n"
        if [[ $? != 0 ]]; then
            LOGE "已取消"
            if [[ $# == 0 ]]; then
                before_show_menu
            fi
            return 0
        fi
    fi
    NOVAS_AUTO_UPGRADE=1 run_install_script
    if [[ $? == 0 ]]; then
        LOGI "更新完成，面板已自动重启"
        return 0
    fi
    return 1
}

write_update_status() {
    local state="$1"
    local message="${2:-}"
    mkdir -p "${NOVAS_UPDATE_STATE_DIR}"
    {
        printf 'state=%s\n' "${state}"
        printf 'updated_at=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
        printf 'message=%s\n' "${message}"
    } > "${NOVAS_UPDATE_STATUS}"
}

run_update_worker() {
	trap 'rm -rf "${NOVAS_UPDATE_LOCK_DIR}"; rm -f "${NOVAS_UPDATE_PID}" "${NOVAS_UPDATE_UNIT_FILE}"' EXIT
	write_update_status "running" "正在后台更新"
	NOVAS_AUTO_UPGRADE=1 update
    local status=$?
    if [[ ${status} -eq 0 ]]; then
        write_update_status "success" "更新完成，面板已重启"
    else
        write_update_status "failed" "更新失败，详见 ${NOVAS_UPDATE_LOG}"
    fi
	return ${status}
}

is_update_running() {
	local value="$(cat "${NOVAS_UPDATE_PID}" 2>/dev/null || true)"
	if [[ "${value}" == systemd:* ]]; then
		local unit="${value#systemd:}"
		[[ -n "${unit}" ]] && systemctl is-active --quiet "${unit}"
		return $?
	fi
	[[ "${value}" =~ ^[0-9]+$ ]] && kill -0 "${value}" 2>/dev/null
}

start_background_update() {
	mkdir -p "${NOVAS_UPDATE_STATE_DIR}"
	exec 8>"${NOVAS_UPDATE_STATE_DIR}/update-launch.lock"
	flock -n 8 || { LOGI "已有更新任务正在启动"; return 0; }
	if [[ -d "${NOVAS_UPDATE_LOCK_DIR}" ]]; then
		if is_update_running; then
			local pid
			pid=$(cat "${NOVAS_UPDATE_PID}" 2>/dev/null || true)
			LOGI "已有更新任务运行中，PID=${pid}"
			return 0
		fi
		rm -rf "${NOVAS_UPDATE_LOCK_DIR}"
		rm -f "${NOVAS_UPDATE_PID}"
	fi
	if ! mkdir "${NOVAS_UPDATE_LOCK_DIR}" 2>/dev/null; then
		LOGI "已有更新任务正在启动，请稍后查看状态"
		return 0
	fi
	if [[ -f "${NOVAS_UPDATE_PID}" ]]; then
		local pid
		pid=$(cat "${NOVAS_UPDATE_PID}")
		if is_update_running; then
			LOGI "已有更新任务运行中，PID=${pid}"
			return 0
		fi
		rm -f "${NOVAS_UPDATE_PID}"
	fi
	: > "${NOVAS_UPDATE_LOG}"
	write_update_status "queued" "已提交后台更新任务"
	local worker_unit="novas-update"
	local worker_command
	printf -v worker_command 'exec >>%q 2>&1; exec /bin/bash %q update --worker' "${NOVAS_UPDATE_LOG}" "$0"
	if command -v systemd-run >/dev/null 2>&1 && systemd-run --unit="${worker_unit}" --collect --no-block /bin/bash -c "${worker_command}" >/dev/null; then
		printf '%s\n' "systemd:${worker_unit}" > "${NOVAS_UPDATE_PID}"
		printf '%s\n' "${worker_unit}" > "${NOVAS_UPDATE_UNIT_FILE}"
		local display_pid="systemd:${worker_unit}"
	else
		nohup setsid bash "$0" update --worker >> "${NOVAS_UPDATE_LOG}" 2>&1 < /dev/null &
		local pid=$!
		printf '%s\n' "${pid}" > "${NOVAS_UPDATE_PID}"
		rm -f "${NOVAS_UPDATE_UNIT_FILE}"
		local display_pid="${pid}"
	fi
	LOGI "后台更新已启动，PID=${display_pid}"
    LOGI "日志：${NOVAS_UPDATE_LOG}"
}

show_update_status() {
    if [[ -f "${NOVAS_UPDATE_STATUS}" ]]; then
        cat "${NOVAS_UPDATE_STATUS}"
    else
        echo "state=never"
    fi
    if [[ -f "${NOVAS_UPDATE_LOG}" ]]; then
        echo "--- 最近日志 ---"
        tail -n 30 "${NOVAS_UPDATE_LOG}"
    fi
}

custom_version() {
    echo "请输入面板版本（例如 v1.4.1）："
    read panel_version

    if [ -z "$panel_version" ]; then
        echo "面板版本不能为空。正在退出。"
    exit 1
    fi

    [[ "${panel_version}" != v* ]] && panel_version="v${panel_version}"

    echo "正在下载并安装 NovaPanel 版本 $panel_version..."
    run_install_script "$panel_version"
}

download_install_script() {
    local installer_path
    installer_path=$(mktemp /tmp/novapanel-install.XXXXXX.sh) || return 1
    if ! curl -fsSL "${NOVAS_INSTALL_SCRIPT_URL}" -o "${installer_path}"; then
        rm -f "${installer_path}"
        return 1
    fi
    chmod +x "${installer_path}"
    printf '%s\n' "${installer_path}"
}

run_install_script() {
    local target_version="${1:-}"
    local installer_path
    installer_path=$(download_install_script)
    if [[ -z "${installer_path}" ]]; then
        LOGE "下载安装脚本失败，请检查当前机器是否可以连接 Github"
        return 1
    fi

    if [[ -n "${target_version}" ]]; then
        bash "${installer_path}" "${target_version}"
    else
        bash "${installer_path}"
    fi
    local status=$?
    rm -f "${installer_path}"
    return ${status}
}

uninstall() {
    confirm "确定要卸载面板吗？" "n"
    if [[ $? != 0 ]]; then
        if [[ $# == 0 ]]; then
            show_menu
        fi
        return 0
    fi
    systemctl stop novas
    systemctl disable novas

    if [[ -x /usr/local/novas/scripts/hy2-forward.sh ]]; then
        /usr/local/novas/scripts/hy2-forward.sh purge || true
    fi
    if [[ -x /usr/local/novas/scripts/login-guard.sh ]]; then
        /usr/local/novas/scripts/login-guard.sh remove || true
    fi

    rm /etc/systemd/system/novas.service -f
    systemctl daemon-reload
    systemctl reset-failed
    rm /etc/novas/ -rf
    rm /usr/local/novas/ -rf

    echo ""
    rm -f /usr/bin/novas
    systemctl daemon-reload
    echo -e "NovaPanel 已卸载。"
    echo ""

    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

reset_admin() {
    echo "不建议将管理员账号密码设置为默认值！"
    confirm "确定要将管理员账号密码重置为默认值吗？" "n"
    if [[ $? == 0 ]]; then
        /usr/local/novas/novas admin -reset
    fi
    before_show_menu
}

set_admin() {
    echo "不建议将管理员账号密码设置为过于复杂的文本。"
    read -p "请设置用户名：" config_account
    read -p "请设置密码：" config_password
    /usr/local/novas/novas admin -username ${config_account} -password ${config_password}
    before_show_menu
}

view_admin() {
    /usr/local/novas/novas admin -show
    before_show_menu
}

reset_setting() {
    confirm "确定要将设置重置为默认值吗？" "n"
    if [[ $? == 0 ]]; then
        /usr/local/novas/novas setting -reset
    fi
    before_show_menu
}

set_setting() {
    echo -e "请输入${yellow}面板端口${plain}（留空则使用现有/默认值）："
    read config_port
    echo -e "请输入${yellow}面板路径${plain}（留空则使用现有/默认值）："
    read config_path

    echo -e "请输入${yellow}订阅端口${plain}（留空则使用现有/默认值）："
    read config_subPort
    echo -e "请输入${yellow}订阅路径${plain}（留空则使用现有/默认值）："
    read config_subPath

    echo -e "${yellow}正在初始化，请稍候...${plain}"
    params=""
    [ -z "$config_port" ] || params="$params -port $config_port"
    [ -z "$config_path" ] || params="$params -path $config_path"
    [ -z "$config_subPort" ] || params="$params -subPort $config_subPort"
    [ -z "$config_subPath" ] || params="$params -subPath $config_subPath"
    if /usr/local/novas/novas setting ${params}; then
        restart novas 0
    else
        LOGE "面板设置保存失败，请检查上方错误信息"
    fi
    before_show_menu
}

view_setting() {
    /usr/local/novas/novas setting -show
    view_uri
    before_show_menu
}

view_uri() {
    info=$(/usr/local/novas/novas uri)
    if [[ $? != 0 ]]; then
        LOGE "获取当前 URI 失败"
        before_show_menu
    fi
    LOGI "你可以通过以下 URL 访问面板："
    echo -e "${green}${info}${plain}"
}

start() {
    check_status $1
    if [[ $? == 0 ]]; then
        echo ""
        LOGI -e "${1} 正在运行，无需再次启动；如果需要重启，请选择重启"
    else
        systemctl start $1
        sleep 2
        check_status $1
        if [[ $? == 0 ]]; then
            LOGI "${1} 启动成功"
        else
            LOGE "启动 ${1} 失败，可能是启动时间超过两秒，请稍后查看日志信息"
        fi
    fi

    if [[ $# == 1 ]]; then
        before_show_menu
    fi
}

stop() {
    check_status $1
    if [[ $? == 1 ]]; then
        echo ""
        LOGI "${1} 已停止，无需再次停止！"
    else
        systemctl stop $1
        sleep 2
        check_status
        if [[ $? == 1 ]]; then
            LOGI "${1} 停止成功"
        else
            LOGE "停止 ${1} 失败，可能是停止时间超过两秒，请稍后查看日志信息"
        fi
    fi

    if [[ $# == 1 ]]; then
        before_show_menu
    fi
}

restart() {
    local service_name="$1"
    local result=0

    if ! systemctl restart "${service_name}"; then
        LOGE "重启 ${service_name} 失败，systemctl 返回错误"
        result=1
    fi
    sleep 2
    check_status "${service_name}"
    if [[ $? == 0 && ${result} -eq 0 ]]; then
        LOGI "${service_name} 重启成功"
    else
        LOGE "重启 ${service_name} 失败，可能是启动时间超过两秒，请稍后查看日志信息"
        result=1
    fi
    if [[ $# == 1 ]]; then
        before_show_menu
    fi
    return ${result}
}

status() {
    systemctl status novas -l
    if [[ $# == 0 ]]; then
        before_show_menu
    fi
}

enable() {
    systemctl enable $1
    if [[ $? == 0 ]]; then
        LOGI "已成功设置 ${1} 开机自启"
    else
        LOGE "设置 ${1} 开机自启失败"
    fi

    if [[ $# == 1 ]]; then
        before_show_menu
    fi
}

disable() {
    systemctl disable $1
    if [[ $? == 0 ]]; then
        LOGI "已成功取消 ${1} 开机自启"
    else
        LOGE "取消 ${1} 开机自启失败"
    fi

    if [[ $# == 1 ]]; then
        before_show_menu
    fi
}

show_log() {
    journalctl -u $1.service -e --no-pager -f
    if [[ $# == 1 ]]; then
        before_show_menu
    fi
}

update_shell() {
    local temp_script
    temp_script=$(mktemp /usr/bin/.novas.XXXXXX) || return 1
    if ! curl -fsSL --max-time 60 https://raw.githubusercontent.com/CatMsg/NovaPanel/main/novas.sh -o "$temp_script" || ! bash -n "$temp_script"; then
        rm -f "$temp_script"
        echo ""
        LOGE "下载脚本失败，请检查当前机器是否可以连接 Github"
        before_show_menu
    else
        chmod 755 "$temp_script"
        mv -f "$temp_script" /usr/bin/novas
        LOGI "脚本升级成功，请重新运行脚本" && exit 0
    fi
}

check_status() {
    if [[ ! -f "/etc/systemd/system/$1.service" ]]; then
        return 2
    fi
    temp=$(systemctl status "$1" | grep Active | awk '{print $3}' | cut -d "(" -f2 | cut -d ")" -f1)
    if [[ x"${temp}" == x"running" ]]; then
        return 0
    else
        return 1
    fi
}

check_enabled() {
    temp=$(systemctl is-enabled $1)
    if [[ x"${temp}" == x"enabled" ]]; then
        return 0
    else
        return 1
    fi
}

check_uninstall() {
    check_status novas
    if [[ $? != 2 ]]; then
        echo ""
        LOGE "面板已安装，请勿重复安装"
        if [[ $# == 0 ]]; then
            before_show_menu
        fi
        return 1
    else
        return 0
    fi
}

check_install() {
    check_status novas
    if [[ $? == 2 ]]; then
        echo ""
        LOGE "请先安装面板"
        if [[ $# == 0 ]]; then
            before_show_menu
        fi
        return 1
    else
        return 0
    fi
}

show_status() {
    check_status $1
    case $? in
    0)
        echo -e "${1} 状态：${green}运行中${plain}"
        show_enable_status $1
        ;;
    1)
        echo -e "${1} 状态：${yellow}未运行${plain}"
        show_enable_status $1
        ;;
    2)
        echo -e "${1} 状态：${red}未安装${plain}"
        ;;
    esac
}

show_enable_status() {
    check_enabled $1
    if [[ $? == 0 ]]; then
        echo -e "${1} 开机自启：${green}是${plain}"
    else
        echo -e "${1} 开机自启：${red}否${plain}"
    fi
}

check_novas_status() {
    count=$(ps -ef | grep "novas" | grep -v "grep" | wc -l)
    if [[ count -ne 0 ]]; then
        return 0
    else
        return 1
    fi
}

show_novas_status() {
    check_novas_status
    if [[ $? == 0 ]]; then
        echo -e "NovaPanel 状态：${green}运行中${plain}"
    else
        echo -e "NovaPanel 状态：${red}未运行${plain}"
    fi
}

bbr_menu() {
    echo -e "${green}\t1.${plain} 启用 BBR"
    echo -e "${green}\t2.${plain} 禁用 BBR"
    echo -e "${green}\t0.${plain} 返回主菜单"
    read -p "请选择一个选项： " choice
    case "$choice" in
    0)
        show_menu
        ;;
    1)
        enable_bbr
        ;;
    2)
        disable_bbr
        ;;
    *) echo "无效选择" ;;
    esac
}

disable_bbr() {
    if ! grep -q "net.core.default_qdisc=fq" /etc/sysctl.conf || ! grep -q "net.ipv4.tcp_congestion_control=bbr" /etc/sysctl.conf; then
        echo -e "${yellow}当前未启用 BBR。${plain}"
        exit 0
    fi
    sed -i 's/net.core.default_qdisc=fq/net.core.default_qdisc=pfifo_fast/' /etc/sysctl.conf
    sed -i 's/net.ipv4.tcp_congestion_control=bbr/net.ipv4.tcp_congestion_control=cubic/' /etc/sysctl.conf
    sysctl -p
    if [[ $(sysctl net.ipv4.tcp_congestion_control | awk '{print $3}') == "cubic" ]]; then
        echo -e "${green}已成功将 BBR 替换为 CUBIC。${plain}"
    else
        echo -e "${red}将 BBR 替换为 CUBIC 失败。请检查系统配置。${plain}"
    fi
}

enable_bbr() {
    if grep -q "net.core.default_qdisc=fq" /etc/sysctl.conf && grep -q "net.ipv4.tcp_congestion_control=bbr" /etc/sysctl.conf; then
        echo -e "${green}BBR 已启用！${plain}"
        exit 0
    fi
    case "${release}" in
    ubuntu | debian | armbian)
        apt-get update && apt-get install -yqq --no-install-recommends ca-certificates
        ;;
    centos | almalinux | rocky | oracle)
        yum -y update && yum -y install ca-certificates
        ;;
    fedora)
        dnf -y update && dnf -y install ca-certificates
        ;;
    arch | manjaro | parch)
        pacman -Sy --noconfirm ca-certificates
        ;;
    *)
        echo -e "${red}不支持的操作系统。请检查脚本并手动安装必要的软件包。${plain}\n"
        exit 1
        ;;
    esac
    echo "net.core.default_qdisc=fq" | tee -a /etc/sysctl.conf
    echo "net.ipv4.tcp_congestion_control=bbr" | tee -a /etc/sysctl.conf
    sysctl -p
    if [[ $(sysctl net.ipv4.tcp_congestion_control | awk '{print $3}') == "bbr" ]]; then
        echo -e "${green}BBR 启用成功。${plain}"
    else
        echo -e "${red}启用 BBR 失败。请检查系统配置。${plain}"
    fi
}

install_cron_package() {
    local package_manager="$1"
    local package_name="$2"

    case "${package_manager}" in
        apt-get)
            apt-get install -y --no-install-recommends "${package_name}"
            ;;
        yum | dnf)
            "${package_manager}" install -y "${package_name}"
            ;;
        pacman)
            pacman -S --noconfirm "${package_name}"
            ;;
        *)
            LOGE "不支持的 cron 包管理器：${package_manager}"
            return 1
            ;;
    esac
}

verify_sysv_cron_boot_links() {
    local service_name="$1"
    local runlevel_dir="$2"
    local link=""
    local init_script="${runlevel_dir%/*}/init.d/${service_name}"

    [[ -x "${init_script}" ]] || return 1
    for link in "${runlevel_dir}"/S[0-9][0-9]"${service_name}"; do
        if [[ -L "${link}" && "${link}" -ef "${init_script}" ]]; then
            return 0
        fi
    done
    return 1
}

enable_sysv_cron() {
    local service_name="$1"
    local runlevel=""

    case "${release}" in
        ubuntu | debian | armbian)
            if ! command -v update-rc.d >/dev/null 2>&1 || \
                ! update-rc.d "${service_name}" defaults || ! update-rc.d "${service_name}" enable; then
                LOGE "无法通过 update-rc.d 持久启用 cron，已停止自动续签配置"
                return 1
            fi
            ;;
        centos | almalinux | rocky | oracle | fedora)
            if ! command -v chkconfig >/dev/null 2>&1 || \
                ! chkconfig --add "${service_name}" || ! chkconfig --level 2345 "${service_name}" on; then
                LOGE "无法通过 chkconfig 持久启用 cron，已停止自动续签配置"
                return 1
            fi
            ;;
        *)
            LOGE "无法验证 ${release} 的 SysV cron 开机启动，请先配置受支持的初始化系统"
            return 1
            ;;
    esac
    for runlevel in 2 3 4 5; do
        if ! verify_sysv_cron_boot_links "${service_name}" "/etc/rc${runlevel}.d"; then
            LOGE "无法验证 cron 开机启动链接 /etc/rc${runlevel}.d，请检查初始化配置后重试"
            return 1
        fi
    done
}

start_cron_service() {
    local service_name="$1"
    local backend="${2:-}"

    if [[ -z "${backend}" ]]; then
        backend=$(cron_service_backend) || {
            LOGE "未检测到可用的初始化系统，无法启用 cron"
            return 1
        }
    fi

    case "${backend}" in
        systemd)
            if ! systemctl enable --now "${service_name}" >/dev/null || ! systemctl is-active --quiet "${service_name}"; then
                LOGE "cron 服务 ${service_name} 未能通过 systemd 启动并保持运行"
                return 1
            fi
            ;;
        openrc)
            if ! rc-update add "${service_name}" default >/dev/null 2>&1 || ! rc-service "${service_name}" start || ! rc-service "${service_name}" status >/dev/null 2>&1; then
                LOGE "cron 服务 ${service_name} 未能通过 OpenRC 启动并保持运行"
                return 1
            fi
            ;;
        service)
            enable_sysv_cron "${service_name}" || return 1
            if ! service "${service_name}" start || ! service "${service_name}" status >/dev/null 2>&1; then
                LOGE "cron 服务 ${service_name} 未能通过 service 启动并保持运行"
                return 1
            fi
            ;;
        *)
            LOGE "未检测到可用的 systemd、OpenRC 或 service 初始化系统，无法启用 cron"
            return 1
            ;;
    esac
    return 0
}

cron_service_backend() {
    local systemd_runtime_dir="${NOVAS_SYSTEMD_RUNTIME_DIR:-/run/systemd/system}"

    if command -v systemctl >/dev/null 2>&1 && [[ -d "${systemd_runtime_dir}" ]]; then
        printf '%s\n' systemd
    elif command -v rc-service >/dev/null 2>&1 && command -v rc-update >/dev/null 2>&1; then
        printf '%s\n' openrc
    elif command -v service >/dev/null 2>&1; then
        printf '%s\n' service
    else
        return 1
    fi
}

cron_service_is_active() {
    local backend="$1"
    local service_name="$2"

    case "${backend}" in
        systemd) systemctl is-active --quiet "${service_name}" ;;
        openrc) rc-service "${service_name}" status >/dev/null 2>&1 ;;
        service) service "${service_name}" status >/dev/null 2>&1 ;;
        *) return 1 ;;
    esac
}

is_exact_no_crontab_error() {
    [[ "$1" =~ ^no[[:space:]]crontab[[:space:]]for[[:space:]][A-Za-z0-9_.-]+$ ]]
}

verify_crontab_usable() {
    local quiet="${1:-0}"
    local temp_dir=""
    local crontab_error=""
    local status=0

    if ! command -v crontab >/dev/null 2>&1; then
        [[ "${quiet}" == "1" ]] || LOGE "cron 已启动，但未找到可用的 crontab 命令"
        return 1
    fi
    temp_dir=$(mktemp -d "${TMPDIR:-/tmp}/novas-crontab-check.XXXXXX") || {
        [[ "${quiet}" == "1" ]] || LOGE "无法安全检查当前 crontab"
        return 1
    }

    if LC_ALL=C crontab -l >"${temp_dir}/listing" 2>"${temp_dir}/error"; then
        status=0
    else
        status=$?
    fi
    crontab_error=$(<"${temp_dir}/error")
    rm -rf "${temp_dir}"

    if [[ ${status} -eq 0 ]] || is_exact_no_crontab_error "${crontab_error}"; then
        return 0
    fi
    [[ "${quiet}" == "1" ]] || LOGE "无法安全读取当前 crontab，拒绝继续安装自动续签"
    return 1
}

ensure_acme_cron() {
    local package_manager=""
    local package_name=""
    local service_name=""
    local service_backend=""
    local crontab_usable=0

    case "${release}" in
        ubuntu | debian | armbian)
            package_manager="apt-get"
            package_name="cron"
            service_name="cron"
            ;;
        centos | almalinux | rocky | oracle)
            if command -v dnf >/dev/null 2>&1; then
                package_manager="dnf"
            else
                package_manager="yum"
            fi
            package_name="cronie"
            service_name="crond"
            ;;
        fedora)
            package_manager="dnf"
            package_name="cronie"
            service_name="crond"
            ;;
        arch | manjaro | parch)
            package_manager="pacman"
            package_name="cronie"
            service_name="cronie"
            ;;
        *)
            LOGE "暂不支持为 ${release} 自动安装 cron，请先配置可用的 cron 服务"
            return 1
            ;;
    esac

    service_backend=$(cron_service_backend) || {
        LOGE "未检测到可用的 systemd、OpenRC 或 service 初始化系统，无法启用 cron"
        return 1
    }
    if verify_crontab_usable 1; then
        crontab_usable=1
    elif command -v crontab >/dev/null 2>&1; then
        LOGE "crontab 命令存在但无法安全读取，拒绝覆盖或重装现有计划任务"
        return 1
    fi
    if [[ "${crontab_usable}" == "1" ]] && cron_service_is_active "${service_backend}" "${service_name}"; then
        if ! start_cron_service "${service_name}" "${service_backend}"; then
            return 1
        fi
        verify_crontab_usable
        return $?
    fi

    if ! command -v "${package_manager}" >/dev/null 2>&1; then
        LOGE "未找到包管理器 ${package_manager}，无法安装 cron"
        return 1
    fi
    if ! install_cron_package "${package_manager}" "${package_name}"; then
        LOGE "安装 cron 软件包 ${package_name} 失败"
        return 1
    fi
    if ! start_cron_service "${service_name}" "${service_backend}"; then
        return 1
    fi
    verify_crontab_usable
}

verify_acme_renewal_cron() {
    local quiet="${1:-0}"
    local temp_dir=""
    local acme_home="${HOME}/.acme.sh"

    if ! command -v crontab >/dev/null 2>&1; then
        [[ "${quiet}" == "1" ]] || LOGE "未找到 crontab，无法验证 acme.sh 自动续签任务"
        return 1
    fi
    temp_dir=$(mktemp -d "${TMPDIR:-/tmp}/novas-acme-cron-check.XXXXXX") || {
        [[ "${quiet}" == "1" ]] || LOGE "无法安全验证 acme.sh 自动续签任务"
        return 1
    }
    if ! LC_ALL=C crontab -l >"${temp_dir}/listing" 2>"${temp_dir}/error"; then
        rm -rf "${temp_dir}"
        [[ "${quiet}" == "1" ]] || LOGE "无法读取 crontab，不能确认 acme.sh 自动续签任务"
        return 1
    fi
    if awk -v acme_home="${acme_home}" '
        function has_exact_home_argument(tail, quoted_home, plain_home, after) {
            sub(/^[[:space:]]+/, "", tail)
            if (substr(tail, 1, 6) != "--home") {
                return 0
            }
            tail = substr(tail, 7)
            if (tail !~ /^[[:space:]]/) {
                return 0
            }
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
        /^[[:space:]]*#/ { next }
        {
            command = $0
            sub(/^[[:space:]]+/, "", command)
            for (field = 1; field <= 5; field++) {
                if (command !~ /^[^[:space:]]+[[:space:]]+/) {
                    command = ""
                    break
                }
                sub(/^[^[:space:]]+[[:space:]]+/, "", command)
            }
            quoted_script = "\"" acme_home "\"/acme.sh --cron"
            plain_script = acme_home "/acme.sh --cron"
            quoted_home = "\"" acme_home "\""
            plain_home = acme_home
            if (substr(command, 1, length(quoted_script)) == quoted_script) {
                tail = substr(command, length(quoted_script) + 1)
                if (has_exact_home_argument(tail, quoted_home, plain_home)) {
                    found = 1
                }
            } else if (substr(command, 1, length(plain_script)) == plain_script) {
                tail = substr(command, length(plain_script) + 1)
                if (has_exact_home_argument(tail, quoted_home, plain_home)) {
                    found = 1
                }
            }
        }
        END { exit !found }
    ' "${temp_dir}/listing"; then
        rm -rf "${temp_dir}"
        return 0
    fi
    rm -rf "${temp_dir}"
    [[ "${quiet}" == "1" ]] || LOGE "crontab 中未找到有效的 acme.sh --cron 续签任务"
    return 1
}

prepare_cloudflare_acme() {
    local acme_bin="${HOME}/.acme.sh/acme.sh"

    if ! ensure_acme_cron; then
        return 1
    fi
    if [[ ! -x "${acme_bin}" ]]; then
        if ! ( unset CF_Token CF_Account_ID CF_Key CF_Email CF_Zone_ID; install_acme_payload ); then
            return 1
        fi
    fi
    if [[ ! -x "${acme_bin}" ]]; then
        LOGE "acme.sh 安装后仍不可执行，已停止证书签发"
        return 1
    fi
    if ! verify_acme_renewal_cron 1; then
        if ! ( unset CF_Token CF_Account_ID CF_Key CF_Email CF_Zone_ID; "${acme_bin}" --install-cronjob ); then
            LOGE "acme.sh 自动续签任务安装失败，已停止证书签发"
            return 1
        fi
    fi
    verify_acme_renewal_cron
}

install_acme_payload() {
    local installer_path=""

    installer_path=$(mktemp "${TMPDIR:-/tmp}/novas-acme-install.XXXXXX") || {
        LOGE "创建 acme.sh 安装临时文件失败"
        return 1
    }
    LOGI "正在安装 acme..."
    if ! curl -fsSL --max-time 120 "https://get.acme.sh" -o "${installer_path}"; then
        rm -f "${installer_path}"
        LOGE "安装 acme 失败"
        return 1
    fi
    if ! (cd "${HOME}" && unset CF_Token CF_Account_ID CF_Key CF_Email CF_Zone_ID && sh "${installer_path}"); then
        rm -f "${installer_path}"
        LOGE "安装 acme 失败"
        return 1
    fi
    rm -f "${installer_path}"
    LOGI "安装 acme 成功"
}

install_acme() {
    ensure_acme_cron || return 1
    install_acme_payload
}

ssl_cert_issue_main() {
    echo -e "${green}\t1.${plain} 获取 SSL"
    echo -e "${green}\t2.${plain} 吊销证书"
    echo -e "${green}\t3.${plain} 强制续签"
    echo -e "${green}\t4.${plain} 自签名证书"
    read -p "请选择一个选项： " choice
    case "$choice" in
        1) ssl_cert_issue ;;
        2)
            local domain=""
            read -p "请输入要吊销证书的域名： " domain
            ~/.acme.sh/acme.sh --revoke -d ${domain}
            LOGI "证书已吊销"
            ;;
        3)
            local domain=""
            read -p "请输入要强制续签 SSL 证书的域名： " domain
            ~/.acme.sh/acme.sh --renew -d ${domain} --force ;;
        4)
            generate_self_signed_cert
            ;;
        *) echo "无效选择" ;;
    esac
}

ssl_cert_issue() {
    if ! command -v ~/.acme.sh/acme.sh &>/dev/null; then
        echo "未找到 acme.sh，将进行安装"
        install_acme
        if [ $? -ne 0 ]; then
            LOGE "安装 acme 失败，请检查日志"
            exit 1
        fi
    fi
    case "${release}" in
    ubuntu | debian | armbian)
        apt update && apt install socat -y
        ;;
    centos | almalinux | rocky | oracle)
        yum -y update && yum -y install socat
        ;;
    fedora)
        dnf -y update && dnf -y install socat
        ;;
    arch | manjaro | parch)
        pacman -Sy --noconfirm socat
        ;;
    *)
        echo -e "${red}不支持的操作系统。请检查脚本并手动安装必要的软件包。${plain}\n"
        exit 1
        ;;
    esac
    if [ $? -ne 0 ]; then
        LOGE "安装 socat 失败，请检查日志"
        exit 1
    else
        LOGI "安装 socat 成功..."
    fi

    local domain=""
    read -p "请输入你的域名：" domain
    LOGD "你的域名是：${domain}，正在检查..."
    local currentCert=$(~/.acme.sh/acme.sh --list | tail -1 | awk '{print $1}')

    if [ ${currentCert} == ${domain} ]; then
        local certInfo=$(~/.acme.sh/acme.sh --list)
        LOGE "系统中已存在证书，不能重复签发，当前证书详情："
        LOGI "$certInfo"
        exit 1
    else
        LOGI "你的域名已准备好签发证书..."
    fi

    certPath="/root/cert/${domain}"
    if [ ! -d "$certPath" ]; then
        mkdir -p "$certPath"
    else
        rm -rf "$certPath"
        mkdir -p "$certPath"
    fi

    local WebPort=80
    read -p "请选择使用的端口，默认使用 80 端口：" WebPort
    if [[ ${WebPort} -gt 65535 || ${WebPort} -lt 1 ]]; then
        LOGE "输入的 ${WebPort} 无效，将使用默认端口"
    fi
    LOGI "将使用端口 ${WebPort} 签发证书，请确保该端口已开放..."
    ~/.acme.sh/acme.sh --set-default-ca --server letsencrypt
    ~/.acme.sh/acme.sh --issue -d ${domain} --standalone --httpport ${WebPort}
    if [ $? -ne 0 ]; then
        LOGE "签发证书失败，请检查日志"
        rm -rf ~/.acme.sh/${domain}
        exit 1
    else
        LOGE "证书签发成功，正在安装证书..."
    fi
    ~/.acme.sh/acme.sh --installcert -d ${domain} \
        --key-file /root/cert/${domain}/privkey.pem \
        --fullchain-file /root/cert/${domain}/fullchain.pem

    if [ $? -ne 0 ]; then
        LOGE "安装证书失败，退出"
        rm -rf ~/.acme.sh/${domain}
        exit 1
    else
        LOGI "安装证书成功，正在启用自动续签..."
    fi

    ~/.acme.sh/acme.sh --upgrade --auto-upgrade
    if [ $? -ne 0 ]; then
        LOGE "自动续签失败，证书详情："
        ls -lah cert/*
        chmod 755 $certPath/*
        exit 1
    else
        LOGI "自动续签成功，证书详情："
        ls -lah cert/*
        chmod 755 $certPath/*
    fi
}

resolve_acme_cert_files() {
    local domain="$1"
    local acmeCertDir="${HOME}/.acme.sh/${domain}_ecc"
    local certFile="${acmeCertDir}/fullchain.cer"
    local keyFile="${acmeCertDir}/${domain}.key"

    if [ ! -s "${certFile}" ] || [ ! -s "${keyFile}" ]; then
        acmeCertDir="${HOME}/.acme.sh/${domain}"
        certFile="${acmeCertDir}/fullchain.cer"
        keyFile="${acmeCertDir}/${domain}.key"
    fi

    if [ -s "${certFile}" ] && [ -s "${keyFile}" ]; then
        printf '%s\n%s\n' "${certFile}" "${keyFile}"
        return 0
    fi

    return 1
}

validate_cf_domain() {
    local domain="$1"
    local -a labels=()
    local label=""

    [[ -n "${domain}" && ${#domain} -le 253 ]] || return 1
    IFS='.' read -r -a labels <<< "${domain}"
    ((${#labels[@]} >= 2)) || return 1
    for label in "${labels[@]}"; do
        [[ ${#label} -le 63 && "${label}" =~ ^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?$ ]] || return 1
    done
}

validate_cf_account_id() {
    [[ "$1" =~ ^[A-Fa-f0-9]{32}$ ]]
}

validate_cf_token() {
    [[ -n "$1" && "$1" != *[[:space:][:cntrl:]]* ]]
}

issue_cloudflare_certificate() {
    local acme_bin="$1"
    local domain="$2"
    local token="$3"
    local account_id="$4"
    local force_renew="$5"
    local -a args=(--issue --dns dns_cf -d "${domain}" -d "*.${domain}")

    if [[ "${force_renew}" == "1" ]]; then
        args+=(--force)
    fi
    (
        unset CF_Key CF_Email CF_Zone_ID
        CF_Token="${token}" CF_Account_ID="${account_id}" "${acme_bin}" "${args[@]}" --log
    )
}

run_cloudflare_acme_issue() {
    local domain="$1"
    local token="$2"
    local account_id="$3"
    local force_renew="$4"
    local acme_bin="${HOME}/.acme.sh/acme.sh"

    if ! ( unset CF_Token CF_Account_ID CF_Key CF_Email CF_Zone_ID; "${acme_bin}" --set-default-ca --server letsencrypt ); then
        LOGE "设置默认 CA Let's Encrypt 失败，证书签发已停止"
        return 1
    fi
    issue_cloudflare_certificate "${acme_bin}" "${domain}" "${token}" "${account_id}" "${force_renew}"
}

restore_cloudflare_certificate_backup() {
    local backup_dir="$1"
    local cert_dir="$2"

    if [[ -e "${cert_dir}" || -L "${cert_dir}" ]] || ! mv -T -- "${backup_dir}" "${cert_dir}"; then
        LOGE "无法恢复旧证书；备份保留在 ${backup_dir}，请清理 ${cert_dir} 后手动恢复备份"
        return 1
    fi
}

install_cloudflare_certificate() {
    local acme_bin="$1"
    local domain="$2"
    local cert_root="$3"
    local cert_dir="${cert_root}/${domain}"
    local staging_dir=""
    local backup_dir=""
    local had_backup=0
    local issued_files=""
    local issued_cert=""
    local issued_key=""

    if ! mkdir -p "${cert_root}"; then
        LOGE "创建证书目录失败：${cert_root}"
        return 1
    fi
    issued_files=$(resolve_acme_cert_files "${domain}") || {
        LOGE "未找到 acme.sh 生成的证书文件，保留已有证书"
        return 1
    }
    issued_cert="${issued_files%%$'\n'*}"
    issued_key="${issued_files#*$'\n'}"
    if [[ -z "${issued_cert}" || -z "${issued_key}" || "${issued_key}" == *$'\n'* ]]; then
        LOGE "acme.sh 证书文件列表无效，保留已有证书"
        return 1
    fi
    staging_dir=$(mktemp -d "${cert_root}/.${domain}.new.XXXXXX") || {
        LOGE "创建临时证书目录失败"
        return 1
    }
    if ! cp "${issued_cert}" "${staging_dir}/fullchain.pem" || ! cp "${issued_key}" "${staging_dir}/privkey.pem"; then
        rm -rf "${staging_dir}"
        LOGE "无法暂存 acme.sh 证书文件，保留已有证书"
        return 1
    fi
    if [[ ! -s "${staging_dir}/fullchain.pem" || ! -s "${staging_dir}/privkey.pem" ]] || \
        ! chmod 644 "${staging_dir}/fullchain.pem" || \
        ! chmod 600 "${staging_dir}/privkey.pem" || \
        ! chmod 700 "${staging_dir}"; then
        rm -rf "${staging_dir}"
        LOGE "临时证书文件不完整或权限设置失败，保留已有证书"
        return 1
    fi

    if [[ -e "${cert_dir}" || -L "${cert_dir}" ]]; then
        backup_dir=$(mktemp -d "${cert_root}/.${domain}.old.XXXXXX") || {
            rm -rf "${staging_dir}"
            LOGE "无法为现有证书创建安全回滚目录"
            return 1
        }
        if ! rmdir "${backup_dir}" || ! mv -T -- "${cert_dir}" "${backup_dir}"; then
            rm -rf "${staging_dir}"
            LOGE "无法暂存现有证书，保留原证书"
            return 1
        fi
        had_backup=1
    fi

    if ! mv -T -- "${staging_dir}" "${cert_dir}"; then
        LOGE "证书发布失败"
        if [[ "${had_backup}" == "1" ]]; then
            restore_cloudflare_certificate_backup "${backup_dir}" "${cert_dir}" || return 1
        fi
        rm -rf "${staging_dir}"
        return 1
    fi

    if ! "${acme_bin}" --installcert -d "${domain}" -d "*.${domain}" \
        --fullchain-file "${cert_dir}/fullchain.pem" \
        --key-file "${cert_dir}/privkey.pem" || \
        ! chmod 644 "${cert_dir}/fullchain.pem" || \
        ! chmod 600 "${cert_dir}/privkey.pem" || \
        ! chmod 700 "${cert_dir}" || \
        [[ ! -s "${cert_dir}/fullchain.pem" || ! -s "${cert_dir}/privkey.pem" ]]; then
        if ! rm -rf "${cert_dir}" || [[ -e "${cert_dir}" || -L "${cert_dir}" ]]; then
            LOGE "证书安装失败，无法清理 ${cert_dir}；旧证书备份保留在 ${backup_dir:-无旧备份}，请手动检查并恢复"
            return 1
        fi
        if [[ "${had_backup}" == "1" ]]; then
            restore_cloudflare_certificate_backup "${backup_dir}" "${cert_dir}" || return 1
            LOGE "证书安装失败，已恢复旧证书"
        else
            LOGE "证书安装失败，未有旧证书可恢复"
        fi
        return 1
    fi

    if [[ "${had_backup}" == "1" ]] && ! rm -rf "${backup_dir}"; then
        LOGE "新证书已安装，但旧证书备份未能清理：${backup_dir}"
    fi
    return 0
}

get_current_sub_domain() {
    /usr/local/novas/novas setting -show 2>/dev/null | sed -n 's/^[[:space:]]*Sub Domain:[[:space:]]*//p' | head -n 1 | tr -d '\r'
}

ssl_cert_issue_CF() {
    local choice=""
    local certPath="/root/cert-CF"

    echo -E ""
    LOGD "******使用说明******"
    echo "1) 从 Cloudflare 申请新证书"
    echo "2) 强制续签已有证书"
    echo "3) 清除面板 HTTPS 路径"
    echo "4) 返回菜单"
    read -p "请输入你的选择 [1-4]： " choice

    case "${choice}" in
        1|2)
            local force_renew="0"
            if [[ "${choice}" == "2" ]]; then
                force_renew="1"
                echo "正在强制重新签发 SSL 证书..."
            else
                echo "开始签发 SSL 证书..."
            fi

            confirm "是否确认？[y/n]" "y"
            if [[ $? -ne 0 ]]; then
                show_menu
                return 0
            fi

            if ! prepare_cloudflare_acme; then
                LOGE "自动续签预检失败，未提示 Cloudflare 凭据且未签发证书"
                return 1
            fi

            local cf_domain=""
            local cf_account_id=""
            local cf_token=""
            LOGD "请设置域名："
            read -r -p "请在此输入域名： " cf_domain
            if ! validate_cf_domain "${cf_domain}"; then
                LOGE "域名格式无效，证书签发已停止"
                return 1
            fi

            read -r -p "请输入 Cloudflare Account ID（32 位十六进制）： " cf_account_id
            if ! validate_cf_account_id "${cf_account_id}"; then
                LOGE "Cloudflare Account ID 格式无效，证书签发已停止"
                return 1
            fi

            LOGI "API Token 仅授权目标 Zone，并赋予 Zone > DNS > Edit、Zone > Zone > Read 权限。"
            read -r -s -p "请输入 Cloudflare API Token： " cf_token
            printf '\n'
            if ! validate_cf_token "${cf_token}"; then
                LOGE "Cloudflare API Token 为空或包含无效空白字符，证书签发已停止"
                return 1
            fi

            if ! run_cloudflare_acme_issue "${cf_domain}" "${cf_token}" "${cf_account_id}" "${force_renew}"; then
                LOGE "证书签发失败，保留已有证书"
                return 1
            fi
            local acme_bin="${HOME}/.acme.sh/acme.sh"
            if ! ( unset CF_Token CF_Account_ID CF_Key CF_Email CF_Zone_ID; "${acme_bin}" --upgrade --auto-upgrade ); then
                LOGE "自动更新设置失败，保留已有证书"
                return 1
            fi
            if ! install_cloudflare_certificate "${acme_bin}" "${cf_domain}" "${certPath}"; then
                return 1
            fi

            LOGI "证书已安装，并已开启自动续签。"
            ls -lah "${certPath}/${cf_domain}"

            local panelCertFile=""
            local panelKeyFile=""
            local subCertFile=""
            local subKeyFile=""
            local certFiles=()
            local subCertFiles=()
            local currentSubDomain=""

            if mapfile -t certFiles < <(resolve_acme_cert_files "${cf_domain}"); then
                panelCertFile="${certFiles[0]}"
                panelKeyFile="${certFiles[1]}"
            fi

            currentSubDomain="$(get_current_sub_domain)"
            if [[ -n "${currentSubDomain}" ]]; then
                if [[ "${currentSubDomain}" == "${cf_domain}" ]]; then
                    subCertFile="${panelCertFile}"
                    subKeyFile="${panelKeyFile}"
                elif mapfile -t subCertFiles < <(resolve_acme_cert_files "${currentSubDomain}"); then
                    subCertFile="${subCertFiles[0]}"
                    subKeyFile="${subCertFiles[1]}"
                fi
            fi

            if [[ -n "${panelCertFile}" && -n "${panelKeyFile}" ]]; then
                LOGI "正在自动回填面板 HTTPS 路径..."
                local settingArgs=(/usr/local/novas/novas setting -webCertFile "${panelCertFile}" -webKeyFile "${panelKeyFile}")
                if [[ -n "${subCertFile}" && -n "${subKeyFile}" ]]; then
                    settingArgs+=(-subCertFile "${subCertFile}" -subKeyFile "${subKeyFile}")
                fi
                "${settingArgs[@]}"
                if [[ $? -ne 0 ]]; then
                    LOGE "自动回填面板路径失败，请稍后手动检查设置-界面"
                else
                    LOGI "面板 HTTPS 路径已自动回填："
                    echo -e "${green}${panelCertFile}${plain}"
                    echo -e "${green}${panelKeyFile}${plain}"
                    if [[ -n "${subCertFile}" && -n "${subKeyFile}" ]]; then
                        LOGI "Sub HTTPS 路径已自动回填："
                        echo -e "${green}${subCertFile}${plain}"
                        echo -e "${green}${subKeyFile}${plain}"
                    fi
                    LOGI "正在重启面板以应用新证书..."
                    restart novas 0
                    if [[ $? -ne 0 ]]; then
                        LOGE "面板重启失败，请手动重启服务"
                    fi
                fi
            else
                LOGE "未找到可回填的证书文件，请检查 acme.sh 生成目录"
            fi
            show_menu
            ;;
        3)
            LOGD "准备清除面板和 Sub HTTPS 路径..."
            /usr/local/novas/novas setting -clearWebTLS -clearSubTLS
            if [ $? -ne 0 ]; then
                LOGE "清除 HTTPS 路径失败，请手动检查"
            else
                LOGI "面板和 Sub HTTPS 路径已清除，正在重启面板..."
                restart novas 0
                if [ $? -ne 0 ]; then
                    LOGE "面板重启失败，请手动重启服务"
                fi
            fi
            show_menu
            ;;
        4)
            echo "正在退出..."
            show_menu
            ;;
        *)
            echo "无效选择，请重新选择。"
            show_menu
            ;;
    esac
}

generate_self_signed_cert() {
    cert_dir="/etc/sing-box"
    mkdir -p "$cert_dir"
    LOGI "请选择证书类型："
    echo -e "${green}\t1.${plain} Ed25519（推荐）"
    echo -e "${green}\t2.${plain} RSA 2048"
    echo -e "${green}\t3.${plain} RSA 4096"
    echo -e "${green}\t4.${plain} ECDSA prime256v1"
    echo -e "${green}\t5.${plain} ECDSA secp384r1"
    read -p "请输入你的选择 [1-5，默认 1]： " cert_type
    cert_type=${cert_type:-1}

    case "$cert_type" in
        1)
            algo="ed25519"
            key_opt="-newkey ed25519"
            ;;
        2)
            algo="rsa"
            key_opt="-newkey rsa:2048"
            ;;
        3)
            algo="rsa"
            key_opt="-newkey rsa:4096"
            ;;
        4)
            algo="ecdsa"
            key_opt="-newkey ec -pkeyopt ec_paramgen_curve:prime256v1"
            ;;
        5)
            algo="ecdsa"
            key_opt="-newkey ec -pkeyopt ec_paramgen_curve:secp384r1"
            ;;
        *)
            algo="ed25519"
            key_opt="-newkey ed25519"
            ;;
    esac

    LOGI "正在生成自签名证书（$algo）..."
    sudo openssl req -x509 -nodes -days 3650 $key_opt \
        -keyout "${cert_dir}/self.key" \
        -out "${cert_dir}/self.crt" \
        -subj "/CN=myserver"
    if [[ $? -eq 0 ]]; then
        sudo chmod 600 "${cert_dir}/self."*
        LOGI "自签名证书生成成功！"
        LOGI "证书路径：${cert_dir}/self.crt"
        LOGI "密钥路径：${cert_dir}/self.key"
    else
        LOGE "生成自签名证书失败。"
    fi
    before_show_menu
}

show_usage() {
    echo -e "NovaPanel 控制菜单用法"
    echo -e "------------------------------------------"
    echo -e "子命令："
    echo -e "novas              - 管理员管理脚本"
    echo -e "novas start        - 启动 NovaPanel"
    echo -e "novas stop         - 停止 NovaPanel"
    echo -e "novas restart      - 重启 NovaPanel"
    echo -e "novas status       - 查看 NovaPanel 当前状态"
    echo -e "novas enable       - 启用开机自启"
    echo -e "novas disable      - 禁用开机自启"
    echo -e "novas log          - 查看 NovaPanel 日志"
    echo -e "novas update       - 更新"
    echo -e "novas update --background - 脱离 SSH 后台更新"
    echo -e "novas update-status - 查看后台更新状态和日志"
    echo -e "novas install      - 安装"
    echo -e "novas uninstall    - 卸载"
    echo -e "novas help         - 控制菜单用法"
    echo -e "------------------------------------------"
}

show_menu() {
  echo -e "
  ${green}NovaPanel 管理脚本 ${plain}
---------------------------------------------------------------
  ${green}0.${plain} 退出
---------------------------------------------------------------
  ${green}1.${plain} 安装
  ${green}2.${plain} 更新
  ${green}3.${plain} 自定义版本
  ${green}4.${plain} 卸载
---------------------------------------------------------------
  ${green}5.${plain} 将管理员账号密码重置为默认值
  ${green}6.${plain} 设置管理员账号密码
  ${green}7.${plain} 查看管理员账号密码
---------------------------------------------------------------
  ${green}8.${plain} 重置面板设置
  ${green}9.${plain} 设置面板设置
  ${green}10.${plain} 查看面板设置
---------------------------------------------------------------
  ${green}11.${plain} 启动 NovaPanel
  ${green}12.${plain} 停止 NovaPanel
  ${green}13.${plain} 重启 NovaPanel
  ${green}14.${plain} 查看 NovaPanel 状态
  ${green}15.${plain} 查看 NovaPanel 日志
  ${green}16.${plain} 启用 NovaPanel 开机自启
  ${green}17.${plain} 禁用 NovaPanel 开机自启
---------------------------------------------------------------
  ${green}18.${plain} 启用或禁用 BBR
  ${green}19.${plain} SSL 证书管理
  ${green}20.${plain} Cloudflare SSL 证书
---------------------------------------------------------------
 "
    show_status novas
    echo -e "提示：使用 \`novas\` 管理 NovaPanel。"
    echo && read -p "请输入你的选择 [0-20]： " num

    case "${num}" in
    0)
        exit 0
        ;;
    1)
        check_uninstall && install
        ;;
    2)
        check_install && update
        ;;
    3)
        check_install && custom_version
        ;;
    4)
        check_install && uninstall
        ;;
    5)
        check_install && reset_admin
        ;;
    6)
        check_install && set_admin
        ;;
    7)
        check_install && view_admin
        ;;
    8)
        check_install && reset_setting
        ;;
    9)
        check_install && set_setting
        ;;
    10)
        check_install && view_setting
        ;;
    11)
        check_install && start novas
        ;;
    12)
        check_install && stop novas
        ;;
    13)
        check_install && restart novas
        ;;
    14)
        check_install && status novas
        ;;
    15)
        check_install && show_log novas
        ;;
    16)
        check_install && enable novas
        ;;
    17)
        check_install && disable novas
        ;;
    18)
        bbr_menu
        ;;
    19)
        ssl_cert_issue_main
        ;;
    20)
        ssl_cert_issue_CF
        ;;
    *)
        LOGE "请输入正确的数字 [0-20]"
        ;;
    esac
}

if [[ $# > 0 ]]; then
    case $1 in
    "admin"|"setting"|"uri"|"checkdb"|"healthcheck"|"-v")
        exec /usr/local/novas/novas "$@"
        ;;
    "start")
        check_install 0 && start novas 0
        ;;
    "stop")
        check_install 0 && stop novas 0
        ;;
    "restart")
        check_install 0 && restart novas 0
        ;;
    "status")
        check_install 0 && status 0
        ;;
    "enable")
        check_install 0 && enable novas 0
        ;;
    "disable")
        check_install 0 && disable novas 0
        ;;
    "log")
        check_install 0 && show_log novas 0
        ;;
    "update")
        check_install 0 || exit $?
        if [[ "${2:-}" == "--background" || "${2:-}" == "-d" ]]; then
            start_background_update
        elif [[ "${2:-}" == "--worker" ]]; then
            run_update_worker
        else
            update 0
        fi
        ;;
    "update-status")
        check_install 0 && show_update_status
        ;;
    "install")
        check_uninstall 0 && install 0
        ;;
    "uninstall")
        check_install 0 && uninstall 0
        ;;
    *) show_usage ;;
    esac
else
    show_menu
fi
