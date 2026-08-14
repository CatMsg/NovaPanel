# NovaPanel

NovaPanel 是基于 s-ui 扩展的 Sing-Box 管理面板，提供协议配置、订阅、端口规则、监控与多服务器运维能力。

> **免责声明：** 本项目仅供个人学习与交流使用，请勿用于非法用途。

## 快速了解

| 项目 | 默认值 |
| --- | --- |
| 最新版本 | [GitHub Releases](https://github.com/CatMsg/NovaPanel/releases/latest) |
| 面板 | `http://服务器IP:2095/app/` |
| 订阅 | `http://服务器IP:2096/sub/` |
| 管理命令 | `s-ui` |
| 发布平台 | Linux AMD64 |

## 主要能力

- 管理入站、出站、节点、服务、路由、规则集、DNS、用户和管理员。
- 支持 VLESS、VMess、Trojan、Shadowsocks、Hysteria2、TUIC、Naive、AnyTLS、Mieru、MASQUE 等协议。
- MASQUE 作为多用户入站运行：单 UDP 端口、用户独立密钥与隧道地址、并发会话、流量配额和 Clash/Mihomo 订阅。
- 输出 Link、JSON、Clash/Mihomo 订阅，并支持多服务器订阅聚合。
- 自动同步入站、节点、面板和订阅端口规则，适配 `ufw`、`nftables` 与 `iptables`。
- 提供系统监控、流量统计、在线用户、访问记录、端口诊断和 Telegram 告警。
- 支持数据库备份恢复、恢复前检查、端口规则重建和失败回滚。
- 服务器集合可集中查看远端状态、日志，并执行后台更新或重启。
- 支持多语言、深色/浅色主题、HTTPS、自定义路径和移动端界面。

## 安装

安装最新版本：

```sh
bash <(curl -Ls https://raw.githubusercontent.com/CatMsg/NovaPanel/main/install.sh)
```

安装指定版本：

```sh
bash <(curl -Ls https://raw.githubusercontent.com/CatMsg/NovaPanel/main/install.sh) vx.x.x
```

安装时可设置管理员账号、面板路径和端口。安装后可使用以下命令维护：

| 命令 | 用途 |
| --- | --- |
| `s-ui` | 打开管理菜单 |
| `s-ui update --background` | 在后台更新，SSH 断开后仍会继续 |
| `s-ui update-status` | 查看后台更新状态与日志 |
| `s-ui admin -show` | 查看管理员信息 |
| `s-ui admin -reset` | 重置管理员账号 |
| `s-ui uninstall` | 卸载 NovaPanel |

## 本地开发

```sh
git clone https://github.com/CatMsg/NovaPanel
cd NovaPanel
sh runSUI.sh
```

开发脚本支持：

```text
sh runSUI.sh run       # 构建、启动并跟随日志
sh runSUI.sh restart   # 停止后重新构建并启动
sh runSUI.sh stop      # 停止本地进程
sh runSUI.sh status    # 查看进程和路径
sh runSUI.sh logs      # 查看最近日志
sh runSUI.sh logs -f   # 持续跟随日志
```

## Docker

当前不发布 GHCR 镜像，可从源码自行构建：

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
| `SUI_LOG_LEVEL` | `info` | `debug`、`info`、`warn` 或 `error` |
| `SUI_DEBUG` | `false` | 启用调试模式 |
| `SUI_BIN_FOLDER` | `bin` | 核心文件目录 |
| `SUI_DB_FOLDER` | `db` | 数据库目录 |
| `SINGBOX_API` | 空 | 自定义 Sing-Box API 地址 |

## 相关文档

- [命名规范](NAMING.md)
- [版本发布](https://github.com/CatMsg/NovaPanel/releases)
- [问题反馈](https://github.com/CatMsg/NovaPanel/issues)

## 致谢

- [alireza0](https://github.com/alireza0/)
- [enfein/mieru](https://github.com/enfein/mieru)
