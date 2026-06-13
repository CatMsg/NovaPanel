
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
| 多协议 | Mixed、SOCKS、HTTP、HTTPS、Direct、Redirect、TProxy、VLESS、VMess、Trojan、Shadowsocks、ShadowTLS、Hysteria、Hysteria2、Naive、TUIC |
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

- 提供更完整的协议与节点管理界面。
- 支持订阅聚合、导出和信息头展示。
- 支持流量统计、在线资源和系统监控。
- 支持面板与订阅分离配置，便于不同场景部署。
- 支持 HTTPS 访问、证书配置和自定义路径。
- Hysteria2 的 `server_ports` 端口跳跃只会同步到专用 NAT 转发规则，不会修改 VPS 的全局默认放行策略；支持逗号分隔的单端口和范围写法，例如 `端口1,端口2,端口3-端口4`。安装脚本会先确保系统里有可用的 `ufw`、`nftables` 或 `iptables`，后续脚本会自动适配，并且可以用 `HY2_FORWARD_BACKEND` 强制指定后端。
- 支持通过脚本、Docker 或源码方式部署。

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
