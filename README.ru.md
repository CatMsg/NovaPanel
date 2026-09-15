# NovaPanel

**Язык / Language:** [简体中文](README.md) | [繁體中文](README.zh-TW.md) | [English](README.en.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | **Русский**

NovaPanel — панель управления Sing-Box для настройки протоколов, подписок, правил портов, мониторинга и работы с несколькими серверами.

> **Отказ от ответственности:** проект предназначен только для личного обучения и обмена информацией. Не используйте его в незаконных целях.

## Краткий обзор

| Параметр | Значение по умолчанию |
| --- | --- |
| Последняя версия | [GitHub Releases](https://github.com/CatMsg/NovaPanel/releases/latest) |
| Панель | `http://SERVER_IP:2095/app/` |
| Подписка | `http://SERVER_IP:2096/sub/` |
| Команда управления | `novas` |
| Платформа релиза | Linux AMD64 |

## Возможности

- Управление inbound, outbound, узлами, сервисами, маршрутизацией, наборами правил, DNS, пользователями и администраторами.
- Каталог правил, диагностика совпадений маршрутов, состояние наборов правил, группы политик, цепочки прокси и последовательный failover.
- Поддержка VLESS, VMess, Trojan, Shadowsocks, Hysteria2, TUIC, Naive, AnyTLS, Mieru, MASQUE и других протоколов.
- MASQUE работает как многопользовательский inbound: один UDP-порт, отдельные ключи и туннельные адреса для пользователей, параллельные сессии, квоты трафика и подписки Clash/Mihomo.
- Единые ограничения скорости загрузки и отдачи для пользователя с общей полосой пропускания между несколькими inbound-протоколами и устройствами, включая Mieru и MASQUE.
- Генерация подписок Link, JSON и Clash/Mihomo, а также агрегация подписок с нескольких серверов.
- Автоматическая синхронизация правил портов inbound, узлов, панели и подписок с `ufw`, `nftables` и `iptables`.
- Мониторинг системы, статистика трафика, онлайн-пользователи, история доступа, диагностика портов и уведомления Telegram.
- Резервное копирование и восстановление базы данных, проверка перед восстановлением, перестроение правил портов и откат при ошибке.
- Fleet-управление позволяет централизованно просматривать состояние и журналы удалённых серверов, а также запускать фоновые обновления и перезапуски.
- Поддержка нескольких языков, тёмной/светлой темы, HTTPS, пользовательских путей, мобильного интерфейса и установки PWA на главный экран.

## Установка

Установить последнюю версию:

```sh
bash <(curl -Ls https://raw.githubusercontent.com/CatMsg/NovaPanel/main/install.sh)
```

Установить конкретную версию:

```sh
bash <(curl -Ls https://raw.githubusercontent.com/CatMsg/NovaPanel/main/install.sh) vx.x.x
```

Во время установки можно настроить учётную запись администратора, путь панели и порты. После установки доступны следующие команды:

| Команда | Назначение |
| --- | --- |
| `novas` | Открыть меню управления |
| `novas update --background` | Обновление в фоне, продолжается после разрыва SSH |
| `novas update-status` | Показать состояние и журналы фонового обновления |
| `novas admin -show` | Показать информацию администратора |
| `novas admin -reset` | Сбросить учётную запись администратора |
| `novas uninstall` | Удалить NovaPanel |

### Защита от перебора паролей

На Linux-хостах с systemd установщик автоматически устанавливает и настраивает Fail2ban. После 10 последовательных неудачных попыток входа через веб-интерфейс в течение 10 минут исходный IP блокируется навсегда. Правило применяется только к текущему TCP-порту панели и не блокирует SSH-порт.

- В разделе **Settings -> Interface** управляйте доверенными IP/CIDR обратных прокси и белым списком блокировки входа.
- В разделе **Health Diagnostics -> Login Protection** просматривайте состояние и снимайте блокировку отдельных IP.
- CLI поддерживает `novas security status`, `novas security bans`, `novas security sync` и `novas security unban <IP>`.
- При включённых уведомлениях Telegram сообщение отправляется после достижения порога неудачных попыток входа.

## Локальная разработка

```sh
git clone https://github.com/CatMsg/NovaPanel
cd NovaPanel
sh runNovas.sh
```

Команды скрипта разработки:

```text
sh runNovas.sh run       # Сборка, запуск и просмотр журналов
sh runNovas.sh restart   # Остановка, повторная сборка и перезапуск
sh runNovas.sh stop      # Остановить локальный процесс
sh runNovas.sh status    # Показать процесс и пути
sh runNovas.sh logs      # Показать последние журналы
sh runNovas.sh logs -f   # Непрерывно следить за журналами
```

## Docker

В настоящее время образ GHCR не публикуется. Проект можно собрать из исходного кода:

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

## Переменные окружения

| Переменная | По умолчанию | Описание |
| --- | --- | --- |
| `NOVAS_LOG_LEVEL` | `info` | `debug`, `info`, `warn` или `error` |
| `NOVAS_DEBUG` | `false` | Включить режим отладки |
| `NOVAS_BIN_FOLDER` | `bin` | Каталог файлов ядра |
| `NOVAS_DB_FOLDER` | `db` | Каталог базы данных |
| `SINGBOX_API` | пусто | Пользовательский адрес Sing-Box API |

## Документация

- [Соглашения об именовании](NAMING.md)
- [Релизы](https://github.com/CatMsg/NovaPanel/releases)
- [Issues](https://github.com/CatMsg/NovaPanel/issues)

## Благодарности

- [alireza0](https://github.com/alireza0/)
- [enfein/mieru](https://github.com/enfein/mieru)
