# NovaPanel

**語言 / Language:** [简体中文](README.md) | **繁體中文** | [English](README.en.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Русский](README.ru.md)

NovaPanel 是一個 Sing-Box 管理面板，提供協定設定、訂閱、連接埠規則、監控與多伺服器維運能力。

> **免責聲明：** 本專案僅供個人學習與交流使用，請勿用於非法用途。

## 快速了解

| 項目 | 預設值 |
| --- | --- |
| 最新版本 | [GitHub Releases](https://github.com/CatMsg/NovaPanel/releases/latest) |
| 面板 | `http://伺服器IP:2095/app/` |
| 訂閱 | `http://伺服器IP:2096/sub/` |
| 管理命令 | `novas` |
| 發布平台 | Linux AMD64 |

## 主要功能

- 管理入站、出站、節點、服務、路由、規則集、DNS、使用者和管理員。
- 提供規則目錄、路由命中診斷、規則集執行狀態、策略群組、代理鏈和有序故障切換。
- 支援 VLESS、VMess、Trojan、Shadowsocks、Hysteria2、TUIC、Naive、AnyTLS、Mieru、MASQUE 等協定。
- MASQUE 以多使用者入站執行：單一 UDP 連接埠、使用者獨立金鑰與隧道位址、並行工作階段、流量配額和 Clash/Mihomo 訂閱。
- 使用者管理統一設定上傳、下載限速，同一使用者跨入站協定和多台裝置共享頻寬額度，並涵蓋 Mieru 與 MASQUE。
- 輸出 Link、JSON、Clash/Mihomo 訂閱，並支援多伺服器訂閱彙總。
- 自動同步入站、節點、面板和訂閱連接埠規則，支援 `ufw`、`nftables` 與 `iptables`。
- 提供系統監控、流量統計、線上使用者、存取記錄、連接埠診斷和 Telegram 警示。
- 支援資料庫備份還原、還原前檢查、連接埠規則重建和失敗回復。
- 伺服器集合可集中檢視遠端狀態、日誌，並執行背景更新或重新啟動。
- 支援多語言、深色/淺色主題、HTTPS、自訂路徑、行動版介面和 PWA 主畫面安裝。

## 安裝

安裝最新版本：

```sh
bash <(curl -Ls https://raw.githubusercontent.com/CatMsg/NovaPanel/main/install.sh)
```

安裝指定版本：

```sh
bash <(curl -Ls https://raw.githubusercontent.com/CatMsg/NovaPanel/main/install.sh) vx.x.x
```

安裝時可設定管理員帳號、面板路徑和連接埠。安裝後可使用以下命令維護：

| 命令 | 用途 |
| --- | --- |
| `novas` | 開啟管理選單 |
| `novas update --background` | 在背景更新，SSH 中斷後仍會繼續 |
| `novas update-status` | 檢視背景更新狀態與日誌 |
| `novas admin -show` | 檢視管理員資訊 |
| `novas admin -reset` | 重設管理員帳號 |
| `novas uninstall` | 解除安裝 NovaPanel |

### 登入暴力破解防護

Linux systemd 主機的安裝腳本會自動安裝並設定 Fail2ban。網頁登入在 10 分鐘內連續失敗 10 次後，來源 IP 會被永久封鎖；規則只比對目前面板 TCP 連接埠，不會封鎖 SSH 連接埠。

- 在「設定 -> 介面」維護可信任反向代理 IP/CIDR 和登入封鎖白名單。
- 在「健康診斷 -> 登入防護」檢視狀態並解除單一 IP 的封鎖。
- 命令列支援 `novas security status`、`novas security bans`、`novas security sync` 和 `novas security unban <IP>`。
- 啟用 Telegram 警示後，達到登入失敗門檻時會傳送通知。

## 本機開發

```sh
git clone https://github.com/CatMsg/NovaPanel
cd NovaPanel
sh runNovas.sh
```

開發腳本支援：

```text
sh runNovas.sh run       # 建置、啟動並持續顯示日誌
sh runNovas.sh restart   # 停止後重新建置並啟動
sh runNovas.sh stop      # 停止本機程序
sh runNovas.sh status    # 檢視程序和路徑
sh runNovas.sh logs      # 檢視最近日誌
sh runNovas.sh logs -f   # 持續顯示日誌
```

## Docker

目前不發布 GHCR 映像，可從原始碼自行建置：

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

## 環境變數

| 變數 | 預設值 | 說明 |
| --- | --- | --- |
| `NOVAS_LOG_LEVEL` | `info` | `debug`、`info`、`warn` 或 `error` |
| `NOVAS_DEBUG` | `false` | 啟用偵錯模式 |
| `NOVAS_BIN_FOLDER` | `bin` | 核心檔案目錄 |
| `NOVAS_DB_FOLDER` | `db` | 資料庫目錄 |
| `SINGBOX_API` | 空白 | 自訂 Sing-Box API 位址 |

## 相關文件

- [命名規範](NAMING.md)
- [版本發布](https://github.com/CatMsg/NovaPanel/releases)
- [問題回報](https://github.com/CatMsg/NovaPanel/issues)

## 致謝

- [alireza0](https://github.com/alireza0/)
- [enfein/mieru](https://github.com/enfein/mieru)
