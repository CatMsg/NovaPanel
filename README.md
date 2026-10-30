# NovaPanel

**语言 / Language:** **简体中文** | [繁體中文](README.zh-TW.md) | [English](README.en.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Русский](README.ru.md)

NovaPanel 是一个 Sing-Box 管理面板，提供协议配置、订阅、端口规则、监控与多服务器运维能力。

> **免责声明：** 本项目仅供个人学习与交流使用，请勿用于非法用途。

## 快速了解

| 项目 | 默认值 |
| --- | --- |
| 最新版本 | [GitHub Releases](https://github.com/CatMsg/NovaPanel/releases/latest) |
| 面板 | `http://服务器IP:2095/app/` |
| 订阅 | `http://服务器IP:2096/sub/` |
| 管理命令 | `novas` |
| 发布平台 | Linux AMD64 |

## 主要能力

- 管理入站、出站、节点、服务、路由、规则集、DNS、用户和管理员。
- 提供规则目录、路由命中诊断、规则集运行状态、策略组、代理链和有序故障回退。
- 支持 VLESS、VMess、Trojan、Shadowsocks、Hysteria2、TUIC、Naive、AnyTLS、Mieru、MASQUE 等协议。
- MASQUE 作为多用户入站运行：单 UDP 端口、用户独立密钥与隧道地址、并发会话、流量配额和 Clash/Mihomo 订阅。
- 用户管理统一配置上传、下载限速，同一用户跨入站协议和多台设备共享带宽额度，并覆盖 Mieru 与 MASQUE。
- 输出 Link、JSON、Clash/Mihomo 订阅，并支持多服务器订阅聚合。
- 自动同步入站、节点、面板和订阅端口规则，适配 `ufw`、`nftables` 与 `iptables`。
- 提供系统监控、流量统计、在线用户、访问记录、端口诊断和 Telegram 告警。
- 支持数据库备份恢复、恢复前检查、端口规则重建和失败回滚。
- 服务器集合可集中查看远端状态、日志，并执行后台更新或重启。
- 支持多语言、深色/浅色主题、HTTPS、自定义路径、移动端界面和 PWA 主屏安装。

## 安装

安装命令会先验证 GitHub Actions 来源证明，再执行安装脚本。为兼容尚未发布脚本来源证明的 `v1.6.149`，仅对该版本校验固定的 `install.sh` SHA-256；其他版本必须通过来源证明。默认安装最新版本；如需固定版本，将 `_novas_version='latest'` 改为例如 `_novas_version='v1.6.149'`。

```bash
(
  set -euo pipefail
  _novas_version='latest'
  if [[ "$_novas_version" == latest ]]; then
    _novas_tag="$(curl --proto '=https' --proto-redir '=https' -fsSL https://api.github.com/repos/CatMsg/NovaPanel/releases/latest | sed -nE 's/^[[:space:]]*"tag_name":[[:space:]]*"([^"]+)".*/\1/p' | head -n1)"
  else
    _novas_tag="v${_novas_version#v}"
  fi
  if ! printf '%s\n' "$_novas_tag" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
    echo "Invalid NovaPanel release tag: $_novas_tag" >&2
    exit 1
  fi
  _novas_tmp="$(mktemp -d)"
  trap 'rm -rf "$_novas_tmp"' EXIT
  _novas_script="$_novas_tmp/install.sh"
  _novas_bundle="$_novas_tmp/install.sh.sigstore.json"
  _novas_release="$_novas_tmp/release.json"
  curl --proto '=https' --proto-redir '=https' -fsSL "https://raw.githubusercontent.com/CatMsg/NovaPanel/${_novas_tag}/install.sh" -o "$_novas_script"
  if ! curl --proto '=https' --proto-redir '=https' -fsSL "https://github.com/CatMsg/NovaPanel/releases/download/${_novas_tag}/NovaPanel-install.sh.sigstore.json" -o "$_novas_bundle"; then
    rm -f "$_novas_bundle"
  fi
  curl --proto '=https' --proto-redir '=https' -fsSL "https://api.github.com/repos/CatMsg/NovaPanel/releases/tags/${_novas_tag}" -o "$_novas_release"
  if [[ -s "$_novas_bundle" ]]; then
    _novas_source_digest="$(sed -nE 's/^[[:space:]]*"target_commitish":[[:space:]]*"([0-9a-fA-F]{40})",?$/\1/p' "$_novas_release" | head -n1)"
    if [[ ! "$_novas_source_digest" =~ ^[0-9a-fA-F]{40}$ ]]; then
      echo "Release source commit is invalid; refusing to run the installer." >&2
      exit 1
    fi
    _novas_gh="$(command -v gh || true)"
    if [[ -z "$_novas_gh" ]] || ! "$_novas_gh" attestation verify --help 2>&1 | grep -Fq -- '--bundle' || ! "$_novas_gh" attestation verify --help 2>&1 | grep -Fq -- '--source-digest'; then
      _novas_gh_version='2.103.2'
      _novas_gh_sha256='62ccf747115ddc61237efffdde66a3c8f70ba249433dc64f70801089b59c59c8'
      _novas_gh_archive="$_novas_tmp/gh_${_novas_gh_version}_linux_amd64.tar.gz"
      _novas_gh_member="gh_${_novas_gh_version}_linux_amd64/bin/gh"
      curl --proto '=https' --proto-redir '=https' -fsSL "https://github.com/cli/cli/releases/download/v${_novas_gh_version}/gh_${_novas_gh_version}_linux_amd64.tar.gz" -o "$_novas_gh_archive"
      printf '%s  %s\n' "$_novas_gh_sha256" "$_novas_gh_archive" | sha256sum -c -
      tar -xzf "$_novas_gh_archive" -C "$_novas_tmp" --no-same-owner --no-same-permissions "$_novas_gh_member"
      _novas_gh="$_novas_tmp/$_novas_gh_member"
    fi
    env -u GH_TOKEN -u GITHUB_TOKEN -u GH_ENTERPRISE_TOKEN GH_CONFIG_DIR="$_novas_tmp/gh-config" GH_HOST=github.com \
      "$_novas_gh" attestation verify "$_novas_script" --repo CatMsg/NovaPanel --bundle "$_novas_bundle" \
      --cert-oidc-issuer https://token.actions.githubusercontent.com \
      --signer-workflow CatMsg/NovaPanel/.github/workflows/release.yml \
      --source-ref refs/heads/main --source-digest "$_novas_source_digest" --deny-self-hosted-runners
  else
    _novas_legacy_sha256='71a91cf3e7a9d18a2f803e272d46266b4100162f9bfcbc69991d626f94534583'
    if [[ "$_novas_tag" != 'v1.6.149' ]] || [[ "$(sha256sum "$_novas_script" | awk '{print $1}')" != "$_novas_legacy_sha256" ]]; then
      echo "Release has no installer provenance bundle and is not the pinned v1.6.149 transition; refusing to run the installer." >&2
      exit 1
    fi
    echo "v1.6.149 installer SHA-256 matches the pinned transition digest."
  fi
  bash -n "$_novas_script"
  bash "$_novas_script" "$_novas_tag"
)
```

安装时可设置管理员账号、面板路径和端口。安装后可使用以下命令维护：

| 命令 | 用途 |
| --- | --- |
| `novas` | 打开管理菜单 |
| `novas update --background` | 在后台更新，SSH 断开后仍会继续 |
| `novas update-status` | 查看后台更新状态与日志 |
| `novas admin -show` | 查看管理员信息 |
| `novas admin -reset` | 重置管理员账号 |
| `novas uninstall` | 卸载 NovaPanel |

### 登录防爆破

Linux systemd 主机的安装脚本会自动安装并配置 Fail2ban。网页登录在 10 分钟内失败 3 次后，来源 IP 会被永久封禁；规则只匹配当前面板 TCP 端口，不会封禁 SSH 端口。

- 在“设置 -> 界面”维护可信反向代理 IP/CIDR 和登录封禁白名单。
- 在“健康诊断 -> 登录防爆破”查看状态并解除单个 IP 的封禁。
- 命令行支持 `novas security status`、`novas security bans`、`novas security sync` 和 `novas security unban <IP>`。
- 启用 Telegram 告警后，达到登录失败阈值时会发送通知。

## 本地开发

```sh
git clone https://github.com/CatMsg/NovaPanel
cd NovaPanel
sh runNovas.sh
```

开发脚本支持：

```text
sh runNovas.sh run       # 构建、启动并跟随日志
sh runNovas.sh restart   # 停止后重新构建并启动
sh runNovas.sh stop      # 停止本地进程
sh runNovas.sh status    # 查看进程和路径
sh runNovas.sh logs      # 查看最近日志
sh runNovas.sh logs -f   # 持续跟随日志
```

## Docker

Docker 镜像构建仅支持 Linux/amd64。目前不发布 GHCR 镜像，可从源码自行构建：

```sh
git clone https://github.com/CatMsg/NovaPanel
cd NovaPanel
docker build -t novapanel .

mkdir -p db cert
docker run -d \
  --network host \
  -v "$PWD/db:/app/db" \
  -v "$PWD/cert:/app/cert" \
  --name novapanel \
  --restart unless-stopped \
  novapanel
```

## 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `NOVAS_LOG_LEVEL` | `info` | `debug`、`info`、`warn` 或 `error` |
| `NOVAS_DEBUG` | `false` | 启用调试模式 |
| `NOVAS_BIN_FOLDER` | `bin` | 核心文件目录 |
| `NOVAS_DB_FOLDER` | `db` | 数据库目录 |
| `SINGBOX_API` | 空 | 自定义 Sing-Box API 地址 |

## 相关文档

- [常用操作指南](docs/WORKFLOWS.md)
- [NovaPanel、s-ui、3x-ui 源码对比与取舍](docs/PANEL_COMPARISON.md)
- [命名规范](NAMING.md)
- [版本发布](https://github.com/CatMsg/NovaPanel/releases)
- [问题反馈](https://github.com/CatMsg/NovaPanel/issues)

## 致谢

- [alireza0](https://github.com/alireza0/)
- [enfein/mieru](https://github.com/enfein/mieru)
