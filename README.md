
# NovaPanel

<p align="center">
  NovaPanel 基于 s-ui 修改
</p>


> **免责声明：** 本项目仅供个人学习与交流使用，请勿用于非法用途。

## 项目速览

| 项目 | 说明 |
| --- | --- |
| 最新发布 | [GitHub Releases](https://github.com/CatMsg/NovaPanel/releases/latest) |
| 仓库地址 | [CatMsg/NovaPanel](https://github.com/CatMsg/NovaPanel) |
| 面板路径 | `/app/` |
| 面板端口 | `2095` |
| 订阅路径 | `/sub/` |
| 订阅端口 | `2096` |
| 命令 | `s-ui` |
| 命名规范 | [NAMING.md](NAMING.md) |

## 一眼看懂


| 能力 | 说明 |
| --- | --- |
| 多协议 | Mixed、SOCKS、HTTP、HTTPS、Direct、Redirect、TProxy、VLESS、VMess、Trojan、Shadowsocks、ShadowTLS、Hysteria、Hysteria2、Naive、TUIC、MASQUE |
| 订阅输出 | `link`、`json`、`clash`、`info` |
| 运维面板 | 入站、出站、端点、服务、规则、路由 |
| 可视化 | 客户端、流量、在线状态、系统状态、访问记录 |
| 使用体验 | 多语言、深色/浅色主题、HTTPS 访问 |
| 部署方式 | Linux、Docker、源码构建 |

## 快速开始

## 安装

### Linux

安装最新版本：

```sh
bash <(curl -Ls https://raw.githubusercontent.com/CatMsg/NovaPanel/main/install.sh)
```

### Windows

GitHub Release 提供 Linux 和 Windows 产物；Windows 也可以使用仓库内脚本本地自行构建和安装：

1. 进入 [windows](windows) 目录。
2. 使用 `build-windows.bat` 或 `build-windows.ps1` 构建，脚本会自动修补当前 sing-box 的 Windows 接口兼容问题，并按 release 同款方式生成 `NovaPanel-windows\` 输出目录。
3. 在 `NovaPanel-windows\` 里以管理员身份运行 `install-windows.bat`。
4. 后续通过 `s-ui-windows.bat` 管理服务。


| 入口 | 命令 | 说明 |
| --- | --- | --- |
| 指定版本 | `bash <(curl -Ls https://raw.githubusercontent.com/CatMsg/NovaPanel/main/install.sh) vx.x.x` | 安装固定版本 |
| 后台更新 | `s-ui update --background` | 脱离 SSH 会话执行更新，避免更新重启时连接中断 |
| 更新状态 | `s-ui update-status` | 查看后台更新状态和最近日志 |
| Docker Compose | `docker compose up -d` | 使用容器部署 |
| 源码运行 | `./runSUI.sh` | 


### 源码构建

```sh
git clone https://github.com/CatMsg/NovaPanel
cd NovaPanel
./runSUI.sh
```

如果你想手动拼装前后端，可以先构建前端，再构建后端：

```sh
cd frontend
npm install
npm run build

cd ..
rm -rf web/html/*
cp -R frontend/dist/ web/html/
go build -o sui main.go
./sui
```

## Docker

> Docker 镜像名 `ghcr.io/catmsg/novapanel-app`。

### docker compose

```yaml
services:
  novapanel:
    image: ghcr.io/catmsg/novapanel-app
    container_name: novapanel
    hostname: "novapanel"
    network_mode: host
    volumes:
      - "./db:/app/db"
      - "./cert:/app/cert"
    tty: true
    restart: unless-stopped
    entrypoint: "./entrypoint.sh"
```

```sh
docker compose up -d
```

### docker run

```sh
mkdir -p novapanel && cd novapanel

docker run -itd \
  --network host \
  -v $PWD/db:/app/db \
  -v $PWD/cert:/app/cert \
  --name novapanel \
  --restart=unless-stopped \
  ghcr.io/catmsg/novapanel-app
```

### 自行构建镜像

```sh
git clone https://github.com/CatMsg/NovaPanel
docker build -t novapanel .
```

## 功能亮点

- 提供入站、出站、节点、服务、规则、DNS、管理员等完整面板管理能力。
- 新增端口管理页，可查看当前监听端口、NAT IPv4 / IPv6 规则和端口转发后端状态。
- 面板端口新增、修改会自动同步到本地端口转发规则，减少手工维护。
- Hysteria2 的 `server_ports` 支持单端口、范围和组合写法，例如 `500,900,1000-1400`，恢复备份后会自动重建对应规则。
- 新增 MASQUE 协议支持，面板可直接管理并启动对应服务进程。
- 备份恢复前会检查端口管理后端，自动适配 `ufw`、`nftables` 或 `iptables`，并在冲突场景下尽量保证恢复结果可用。
- 服务器集合支持保存远端 NovaPanel 地址和加密令牌，可集中查看版本、核心、端口、用户、节点和 MASQUE 状态，并查看日志或重启面板。
- 服务器集合支持单台刷新、状态重试、断线保留上次状态，以及通过令牌远程执行脱离 SSH 的 `s-ui update`，可查看更新状态和最近日志。
- 更新安装器会校验发布包 SHA-256，使用临时目录解包，并在安装或启动失败时回滚上一版核心文件。
- 服务器集合配置随数据库备份保存；恢复时会校验 SQLite 完整性、关键数据表和集合配置，失效的本机证书路径会自动清理，并保留恢复前数据库作为回退副本。
- 数据库备份包含服务、API Token 和受管端口规则；恢复前可先预览数据统计，确认后再替换当前数据库。
- 端口管理页支持检查受管端口数量并一键重建端口规则，用于修复防火墙/NAT 规则漂移。
- 入站、节点、面板端口和订阅端口的配置变更采用数据库、核心和端口规则的失败回滚策略，避免只保存了一半导致状态不一致。
- SSL 证书申请成功后可自动回填面板配置路径，并提供重置入口作为兜底。
- 修复配置保存过程中偶发的 `database is locked` 问题，减少保存后重启核心时的并发冲突。
- 首页与登录页已做移动端适配，信息卡和实时状态也针对小屏做了重新整理。
- 支持订阅聚合、导出和信息头展示，支持流量统计、在线资源和系统监控。
- 支持面板与订阅分离配置，便于不同场景部署。
- 支持 HTTPS 访问、自定义路径，以及脚本、Docker 或源码方式部署。

## 默认信息

| 项目 | 默认值 |
| --- | --- |
| 面板端口 | `2095` |
| 面板路径 | `/app/` |
| 订阅端口 | `2096` |
| 订阅路径 | `/sub/` |

首次安装时，脚本会提示你确认或修改管理员账号密码；如果不修改，也可能自动生成一组随机凭据。后续可用 `s-ui admin -show` 查看当前账号信息，`s-ui admin -reset` 恢复为默认值。

## 卸载

```sh
sudo -i

systemctl disable s-ui --now
rm -f /etc/systemd/system/sing-box.service
systemctl daemon-reload

rm -rf /usr/local/s-ui
rm -f /usr/bin/s-ui
```

如果你启用了 Hysteria2 的 `server_ports` 转发，`s-ui uninstall` 会先清掉 NovaPanel 自己写入的专用转发规则，再删除程序目录。

## 语言

- English
- 简体中文

## 环境变量

| 变量 | 类型 | 默认值 |
| --- | --- | --- |
| `SUI_LOG_LEVEL` | `"debug"` \| `"info"` \| `"warn"` \| `"error"` | `"info"` |
| `SUI_DEBUG` | `boolean` | `false` |
| `SUI_BIN_FOLDER` | `string` | `"bin"` |
| `SUI_DB_FOLDER` | `string` | `"db"` |
| `SINGBOX_API` | `string` | - |

## SSL 证书

使用 Certbot 示例：

```bash
snap install core
snap refresh core
snap install --classic certbot
ln -s /snap/bin/certbot /usr/bin/certbot

certbot certonly --standalone --register-unsafely-without-email --non-interactive --agree-tos -d <你的域名>
```

## 致谢

- [alireza0](https://github.com/alireza0/)
