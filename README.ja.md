# NovaPanel

**言語 / Language:** [简体中文](README.md) | [繁體中文](README.zh-TW.md) | [English](README.en.md) | **日本語** | [한국어](README.ko.md) | [Русский](README.ru.md)

NovaPanel は、プロトコル設定、サブスクリプション、ポートルール、監視、複数サーバー運用を提供する Sing-Box 管理パネルです。

> **免責事項:** 本プロジェクトは個人の学習および交流目的のみを想定しています。違法な用途には使用しないでください。

## クイック概要

| 項目 | 既定値 |
| --- | --- |
| 最新バージョン | [GitHub Releases](https://github.com/CatMsg/NovaPanel/releases/latest) |
| パネル | `http://SERVER_IP:2095/app/` |
| サブスクリプション | `http://SERVER_IP:2096/sub/` |
| 管理コマンド | `novas` |
| リリース対象 | Linux AMD64 |

## 主な機能

- Inbound、Outbound、ノード、サービス、ルーティング、Rule Set、DNS、ユーザー、管理者を管理します。
- ルールカタログ、ルート命中診断、Rule Set の実行状態、ポリシーグループ、プロキシチェーン、順序付きフェイルオーバーを提供します。
- VLESS、VMess、Trojan、Shadowsocks、Hysteria2、TUIC、Naive、AnyTLS、Mieru、MASQUE などをサポートします。
- MASQUE はマルチユーザー Inbound として動作し、単一 UDP ポート、ユーザーごとの鍵とトンネルアドレス、同時セッション、トラフィック制限、Clash/Mihomo サブスクリプションを提供します。
- ユーザー単位のアップロード/ダウンロード速度制限を統合し、同一ユーザーの複数 Inbound プロトコル・複数端末で帯域枠を共有します。Mieru と MASQUE も対象です。
- Link、JSON、Clash/Mihomo サブスクリプションを生成し、複数サーバーのサブスクリプション集約にも対応します。
- Inbound、ノード、パネル、サブスクリプションのポートルールを自動同期し、`ufw`、`nftables`、`iptables` に対応します。
- システム監視、トラフィック統計、オンラインユーザー、アクセス履歴、ポート診断、Telegram 通知を提供します。
- データベースのバックアップ/復元、復元前チェック、ポートルール再構築、失敗時ロールバックに対応します。
- Fleet 管理でリモートサーバーの状態とログを一元表示し、バックグラウンド更新や再起動を実行できます。
- 多言語、ダーク/ライトテーマ、HTTPS、カスタムパス、モバイル UI、PWA のホーム画面追加に対応します。

## インストール

最新版をインストール:

```sh
bash <(curl -Ls https://raw.githubusercontent.com/CatMsg/NovaPanel/main/install.sh)
```

特定バージョンをインストール:

```sh
bash <(curl -Ls https://raw.githubusercontent.com/CatMsg/NovaPanel/main/install.sh) vx.x.x
```

インストール時に管理者アカウント、パネルパス、ポートを設定できます。インストール後は次のコマンドで管理できます。

| コマンド | 用途 |
| --- | --- |
| `novas` | 管理メニューを開く |
| `novas update --background` | バックグラウンド更新。SSH 切断後も継続 |
| `novas update-status` | バックグラウンド更新の状態とログを表示 |
| `novas admin -show` | 管理者情報を表示 |
| `novas admin -reset` | 管理者アカウントをリセット |
| `novas uninstall` | NovaPanel をアンインストール |

### ログイン総当たり攻撃対策

Linux systemd 環境では、インストーラーが Fail2ban を自動的にインストールして設定します。Web ログインが 10 分以内に 10 回連続で失敗すると、送信元 IP は恒久的にブロックされます。ルールは現在のパネル TCP ポートだけを対象とし、SSH ポートはブロックしません。

- **設定 -> インターフェース** で信頼するリバースプロキシの IP/CIDR とログインブロック許可リストを管理します。
- **ヘルス診断 -> ログイン保護** で状態確認と個別 IP の解除を行います。
- CLI では `novas security status`、`novas security bans`、`novas security sync`、`novas security unban <IP>` を使用できます。
- Telegram 通知を有効にすると、ログイン失敗のしきい値到達時に通知されます。

## ローカル開発

```sh
git clone https://github.com/CatMsg/NovaPanel
cd NovaPanel
sh runNovas.sh
```

開発スクリプト:

```text
sh runNovas.sh run       # ビルド、起動、ログ追跡
sh runNovas.sh restart   # 停止後に再ビルドして再起動
sh runNovas.sh stop      # ローカルプロセスを停止
sh runNovas.sh status    # プロセスとパス情報を表示
sh runNovas.sh logs      # 最近のログを表示
sh runNovas.sh logs -f   # ログを継続追跡
```

## Docker

現在 GHCR イメージは公開していません。ソースからビルドできます。

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

## 環境変数

| 変数 | 既定値 | 説明 |
| --- | --- | --- |
| `NOVAS_LOG_LEVEL` | `info` | `debug`、`info`、`warn`、`error` |
| `NOVAS_DEBUG` | `false` | デバッグモードを有効化 |
| `NOVAS_BIN_FOLDER` | `bin` | コアファイルのディレクトリ |
| `NOVAS_DB_FOLDER` | `db` | データベースのディレクトリ |
| `SINGBOX_API` | 空 | カスタム Sing-Box API アドレス |

## ドキュメント

- [命名規則](NAMING.md)
- [リリース](https://github.com/CatMsg/NovaPanel/releases)
- [Issue](https://github.com/CatMsg/NovaPanel/issues)

## 謝辞

- [alireza0](https://github.com/alireza0/)
- [enfein/mieru](https://github.com/enfein/mieru)
