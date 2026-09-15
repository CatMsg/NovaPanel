# NovaPanel

**Language:** [简体中文](README.md) | [繁體中文](README.zh-TW.md) | **English** | [日本語](README.ja.md) | [한국어](README.ko.md) | [Русский](README.ru.md)

NovaPanel is a Sing-Box management panel for protocol configuration, subscriptions, port rules, monitoring, and multi-server operations.

> **Disclaimer:** This project is intended for personal learning and communication only. Do not use it for illegal purposes.

## Quick Overview

| Item | Default |
| --- | --- |
| Latest version | [GitHub Releases](https://github.com/CatMsg/NovaPanel/releases/latest) |
| Panel | `http://SERVER_IP:2095/app/` |
| Subscription | `http://SERVER_IP:2096/sub/` |
| Management command | `novas` |
| Release platform | Linux AMD64 |

## Features

- Manage inbounds, outbounds, nodes, services, routing, rule sets, DNS, clients, and administrators.
- Includes a rule catalog, route-hit diagnostics, rule-set runtime status, policy groups, proxy chains, and ordered failover.
- Supports VLESS, VMess, Trojan, Shadowsocks, Hysteria2, TUIC, Naive, AnyTLS, Mieru, MASQUE, and other protocols.
- MASQUE runs as a multi-user inbound with a single UDP port, per-user keys and tunnel addresses, concurrent sessions, traffic quotas, and Clash/Mihomo subscriptions.
- Unified client upload/download rate limits are shared across inbound protocols and multiple devices for the same user, including Mieru and MASQUE.
- Generates Link, JSON, and Clash/Mihomo subscriptions, with multi-server subscription aggregation.
- Automatically synchronizes inbound, node, panel, and subscription port rules with `ufw`, `nftables`, and `iptables`.
- Provides system monitoring, traffic statistics, online-user status, access history, port diagnostics, and Telegram alerts.
- Supports database backup and restore, pre-restore checks, port-rule rebuilds, and rollback on failure.
- Fleet management provides centralized remote status and logs, and can run background updates or restarts.
- Supports multiple UI languages, dark/light themes, HTTPS, custom paths, mobile layouts, and PWA home-screen installation.

## Installation

Install the latest version:

```sh
bash <(curl -Ls https://raw.githubusercontent.com/CatMsg/NovaPanel/main/install.sh)
```

Install a specific version:

```sh
bash <(curl -Ls https://raw.githubusercontent.com/CatMsg/NovaPanel/main/install.sh) vx.x.x
```

During installation you can configure the administrator account, panel path, and ports. After installation, use these commands for maintenance:

| Command | Purpose |
| --- | --- |
| `novas` | Open the management menu |
| `novas update --background` | Update in the background and continue even after SSH disconnects |
| `novas update-status` | View background update status and logs |
| `novas admin -show` | Show administrator information |
| `novas admin -reset` | Reset the administrator account |
| `novas uninstall` | Uninstall NovaPanel |

### Login Brute-force Protection

On Linux systemd hosts, the installer automatically installs and configures Fail2ban. After 10 consecutive failed web logins within 10 minutes, the source IP is permanently banned. The rule only matches the current panel TCP port and does not ban the SSH port.

- Manage trusted reverse-proxy IP/CIDR ranges and the login-ban allowlist under **Settings -> Interface**.
- View protection status and unban individual IP addresses under **Health Diagnostics -> Login Protection**.
- CLI commands include `novas security status`, `novas security bans`, `novas security sync`, and `novas security unban <IP>`.
- When Telegram alerts are enabled, NovaPanel sends a notification after the failed-login threshold is reached.

## Local Development

```sh
git clone https://github.com/CatMsg/NovaPanel
cd NovaPanel
sh runNovas.sh
```

Development script commands:

```text
sh runNovas.sh run       # Build, start, and follow logs
sh runNovas.sh restart   # Stop, rebuild, and restart
sh runNovas.sh stop      # Stop the local process
sh runNovas.sh status    # Show process and path information
sh runNovas.sh logs      # Show recent logs
sh runNovas.sh logs -f   # Follow logs continuously
```

## Docker

NovaPanel does not currently publish a GHCR image. You can build it from source:

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

## Environment Variables

| Variable | Default | Description |
| --- | --- | --- |
| `NOVAS_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |
| `NOVAS_DEBUG` | `false` | Enable debug mode |
| `NOVAS_BIN_FOLDER` | `bin` | Core binary directory |
| `NOVAS_DB_FOLDER` | `db` | Database directory |
| `SINGBOX_API` | empty | Custom Sing-Box API address |

## Documentation

- [Naming conventions](NAMING.md)
- [Releases](https://github.com/CatMsg/NovaPanel/releases)
- [Issue tracker](https://github.com/CatMsg/NovaPanel/issues)

## Acknowledgements

- [alireza0](https://github.com/alireza0/)
- [enfein/mieru](https://github.com/enfein/mieru)
