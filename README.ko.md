# NovaPanel

**언어 / Language:** [简体中文](README.md) | [繁體中文](README.zh-TW.md) | [English](README.en.md) | [日本語](README.ja.md) | **한국어** | [Русский](README.ru.md)

NovaPanel은 프로토콜 구성, 구독, 포트 규칙, 모니터링 및 다중 서버 운영 기능을 제공하는 Sing-Box 관리 패널입니다.

> **면책 조항:** 이 프로젝트는 개인 학습 및 정보 교류 목적으로만 제공됩니다. 불법적인 용도로 사용하지 마십시오.

## 빠른 개요

| 항목 | 기본값 |
| --- | --- |
| 최신 버전 | [GitHub Releases](https://github.com/CatMsg/NovaPanel/releases/latest) |
| 패널 | `http://SERVER_IP:2095/app/` |
| 구독 | `http://SERVER_IP:2096/sub/` |
| 관리 명령 | `novas` |
| 릴리스 플랫폼 | Linux AMD64 |

## 주요 기능

- Inbound, Outbound, 노드, 서비스, 라우팅, Rule Set, DNS, 사용자 및 관리자를 관리합니다.
- 규칙 카탈로그, 라우트 매칭 진단, Rule Set 실행 상태, 정책 그룹, 프록시 체인 및 순차 장애 조치를 제공합니다.
- VLESS, VMess, Trojan, Shadowsocks, Hysteria2, TUIC, Naive, AnyTLS, Mieru, MASQUE 등의 프로토콜을 지원합니다.
- MASQUE는 단일 UDP 포트, 사용자별 키와 터널 주소, 동시 세션, 트래픽 할당량, Clash/Mihomo 구독을 제공하는 다중 사용자 Inbound로 동작합니다.
- 사용자별 업로드/다운로드 속도 제한을 통합하며, 동일 사용자는 여러 Inbound 프로토콜과 여러 장치에서 대역폭 한도를 공유합니다. Mieru와 MASQUE도 포함됩니다.
- Link, JSON, Clash/Mihomo 구독을 생성하고 다중 서버 구독 집계를 지원합니다.
- Inbound, 노드, 패널 및 구독 포트 규칙을 자동으로 동기화하며 `ufw`, `nftables`, `iptables`를 지원합니다.
- 시스템 모니터링, 트래픽 통계, 온라인 사용자, 접속 기록, 포트 진단 및 Telegram 알림을 제공합니다.
- 데이터베이스 백업/복원, 복원 전 검사, 포트 규칙 재구성 및 실패 시 롤백을 지원합니다.
- Fleet 관리에서 원격 서버 상태와 로그를 중앙에서 확인하고 백그라운드 업데이트 또는 재시작을 실행할 수 있습니다.
- 다국어, 다크/라이트 테마, HTTPS, 사용자 지정 경로, 모바일 UI 및 PWA 홈 화면 설치를 지원합니다.

## 설치

최신 버전 설치:

```sh
bash <(curl -Ls https://raw.githubusercontent.com/CatMsg/NovaPanel/main/install.sh)
```

특정 버전 설치:

```sh
bash <(curl -Ls https://raw.githubusercontent.com/CatMsg/NovaPanel/main/install.sh) vx.x.x
```

설치 과정에서 관리자 계정, 패널 경로 및 포트를 설정할 수 있습니다. 설치 후에는 다음 명령으로 관리할 수 있습니다.

| 명령 | 용도 |
| --- | --- |
| `novas` | 관리 메뉴 열기 |
| `novas update --background` | 백그라운드 업데이트, SSH 연결이 끊겨도 계속 실행 |
| `novas update-status` | 백그라운드 업데이트 상태와 로그 확인 |
| `novas admin -show` | 관리자 정보 표시 |
| `novas admin -reset` | 관리자 계정 초기화 |
| `novas uninstall` | NovaPanel 제거 |

### 로그인 무차별 대입 방어

Linux systemd 호스트에서는 설치 프로그램이 Fail2ban을 자동으로 설치하고 구성합니다. 웹 로그인에 10분 이내 10회 연속 실패하면 해당 소스 IP가 영구 차단됩니다. 규칙은 현재 패널 TCP 포트만 대상으로 하며 SSH 포트는 차단하지 않습니다.

- **설정 -> 인터페이스**에서 신뢰할 수 있는 리버스 프록시 IP/CIDR과 로그인 차단 허용 목록을 관리합니다.
- **상태 진단 -> 로그인 보호**에서 상태를 확인하고 개별 IP 차단을 해제합니다.
- CLI에서는 `novas security status`, `novas security bans`, `novas security sync`, `novas security unban <IP>`를 사용할 수 있습니다.
- Telegram 알림을 활성화하면 로그인 실패 임계값에 도달했을 때 알림이 전송됩니다.

## 로컬 개발

```sh
git clone https://github.com/CatMsg/NovaPanel
cd NovaPanel
sh runNovas.sh
```

개발 스크립트 명령:

```text
sh runNovas.sh run       # 빌드, 시작 및 로그 추적
sh runNovas.sh restart   # 중지 후 다시 빌드하여 재시작
sh runNovas.sh stop      # 로컬 프로세스 중지
sh runNovas.sh status    # 프로세스 및 경로 정보 표시
sh runNovas.sh logs      # 최근 로그 표시
sh runNovas.sh logs -f   # 로그 계속 추적
```

## Docker

현재 GHCR 이미지는 배포하지 않습니다. 소스에서 직접 빌드할 수 있습니다.

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

## 환경 변수

| 변수 | 기본값 | 설명 |
| --- | --- | --- |
| `NOVAS_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `NOVAS_DEBUG` | `false` | 디버그 모드 활성화 |
| `NOVAS_BIN_FOLDER` | `bin` | 코어 파일 디렉터리 |
| `NOVAS_DB_FOLDER` | `db` | 데이터베이스 디렉터리 |
| `SINGBOX_API` | 비어 있음 | 사용자 지정 Sing-Box API 주소 |

## 문서

- [명명 규칙](NAMING.md)
- [릴리스](https://github.com/CatMsg/NovaPanel/releases)
- [이슈](https://github.com/CatMsg/NovaPanel/issues)

## 감사의 글

- [alireza0](https://github.com/alireza0/)
- [enfein/mieru](https://github.com/enfein/mieru)
