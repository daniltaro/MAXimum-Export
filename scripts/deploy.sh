#!/usr/bin/env bash
# Разворачивание MAXimum Export на сервер одной командой.
#
#   ./scripts/deploy.sh root@81.200.0.1
#
# Что делает:
#   1) проверяет, что локально всё собирается и тесты проходят;
#   2) копирует на сервер текущую версию из git (без .git, секретов и мусора);
#   3) копирует .env с токеном бота — по шифрованному каналу, в репозиторий он не попадает;
#   4) готовит Docker на сервере: движок и плагины compose и buildx, если их нет;
#   5) собирает образ и запускает бота; после перезагрузки сервера он поднимется сам;
#   6) проверяет, что бот подключился к MAX.
#
# Пароль спрашивается один раз: соединение с сервером переиспользуется (ControlMaster).
# Чтобы не вводить его вовсе: ssh-copy-id пользователь@адрес-сервера
set -euo pipefail

SERVER="${1:-}"
DIR="${2:-/opt/maxexport}"
if [ -z "$SERVER" ]; then
    echo "Использование: $0 пользователь@адрес-сервера [папка на сервере]" >&2
    exit 1
fi

cd "$(dirname "$0")/.."

# Одно SSH-соединение на весь скрипт: пароль вводится один раз.
CTL="/tmp/maxexport-ssh-$$"
SSH=(-o ControlMaster=auto -o ControlPath="$CTL" -o ControlPersist=10m)
TMP=""
cleanup() {
    [ -n "$TMP" ] && rm -rf "$TMP"
    ssh -O exit -o ControlPath="$CTL" "$SERVER" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "== 1/6 Проверка проекта локально"
go vet ./... >/dev/null
go test ./... >/dev/null
echo "   тесты прошли, версия: $(git rev-parse --short HEAD)"

if [ ! -f .env ]; then
    echo "Нет файла .env с токеном бота. Создайте его: cp .env.example .env" >&2
    exit 1
fi

echo "== 2/6 Упаковка версии из git"
TMP="$(mktemp -d)"
git archive --format=tar HEAD -o "$TMP/app.tar"

echo "== 3/6 Копирование на сервер ($SERVER:$DIR)"
ssh "${SSH[@]}" "$SERVER" "mkdir -p $DIR"
scp "${SSH[@]}" -q "$TMP/app.tar" "$SERVER:$DIR/app.tar"
scp "${SSH[@]}" -q .env "$SERVER:$DIR/.env"

echo "== 4/6 Docker на сервере"
ssh "${SSH[@]}" "$SERVER" 'bash -s' <<'REMOTE'
set -euo pipefail

# Версии плагинов Docker. Ставятся одним файлом, без apt — на свежей Ubuntu apt часто
# занят автообновлением (unattended-upgrades держит блокировку dpkg).
COMPOSE_VERSION=v5.5.1
BUILDX_VERSION=v0.37.1
PLUGINS=/usr/local/lib/docker/cli-plugins

# Ждём, пока автообновление Ubuntu отпустит apt: без этого установка падает с
# «Could not get lock /var/lib/dpkg/lock-frontend».
apt_free() { # свободен ли dpkg? fuser есть не на каждом образе, тогда спрашиваем сам apt
    if command -v fuser >/dev/null 2>&1; then
        ! fuser /var/lib/dpkg/lock-frontend >/dev/null 2>&1
    else
        apt-get check >/dev/null 2>&1
    fi
}

wait_apt() {
    for _ in $(seq 60); do
        if apt_free; then
            return 0
        fi
        echo "   жду, пока Ubuntu закончит автообновление…"
        sleep 5
    done
    echo "   apt занят автообновлением дольше 5 минут" >&2
    return 1
}

# Плагин Docker — один исполняемый файл в /usr/local/lib/docker/cli-plugins.
install_plugin() { # $1 — имя (compose|buildx), $2 — ссылка
    mkdir -p "$PLUGINS"
    if curl -fsSL "$2" -o "$PLUGINS/docker-$1.part"; then
        chmod +x "$PLUGINS/docker-$1.part"
        mv "$PLUGINS/docker-$1.part" "$PLUGINS/docker-$1"
        return 0
    fi
    rm -f "$PLUGINS/docker-$1.part"
    return 1
}

# Запасной путь: репозиторий Docker через apt, если файлы с GitHub не скачались.
install_from_apt() {
    wait_apt
    apt-get install -y -qq ca-certificates curl
    install -m 0755 -d /etc/apt/keyrings
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
    chmod a+r /etc/apt/keyrings/docker.asc
    . /etc/os-release
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $VERSION_CODENAME stable" \
        > /etc/apt/sources.list.d/docker.list
    wait_apt
    apt-get update -qq
    apt-get install -y -qq docker-compose-plugin docker-buildx-plugin
}

# 1. Движок Docker.
if ! command -v docker >/dev/null; then
    echo "   ставлю Docker"
    wait_apt
    curl -fsSL https://get.docker.com | sh
fi
systemctl is-active --quiet docker || systemctl start docker

# 2. Архитектура: у compose файлы названы x86_64/aarch64, у buildx — amd64/arm64.
case "$(uname -m)" in
    aarch64 | arm64) COMPOSE_ARCH=aarch64; BUILDX_ARCH=arm64 ;;
    *) COMPOSE_ARCH=x86_64; BUILDX_ARCH=amd64 ;;
esac

# 3. Compose v2. На многих образах Ubuntu стоит только docker-compose 1.x, который не
#    понимает современный compose.yaml («env_file … invalid type») и не знает docker compose.
if ! docker compose version >/dev/null 2>&1; then
    echo "   ставлю Docker Compose v2 (найден только старый docker-compose или его нет)"
    install_plugin compose \
        "https://github.com/docker/compose/releases/download/$COMPOSE_VERSION/docker-compose-linux-$COMPOSE_ARCH" \
        || install_from_apt
fi

# 4. buildx — сборщик образов для compose v2.
if ! docker buildx version >/dev/null 2>&1; then
    echo "   ставлю buildx"
    install_plugin buildx \
        "https://github.com/docker/buildx/releases/download/$BUILDX_VERSION/buildx-$BUILDX_VERSION.linux-$BUILDX_ARCH" \
        || install_from_apt
fi

# 5. Образы для сборки: из России Docker Hub иногда недоступен — тогда включаем зеркало.
#    Заодно прогреваем кэш, чтобы сборка не ждала загрузку.
if ! timeout 180 docker pull -q golang:1.24-alpine >/dev/null 2>&1; then
    echo "   Docker Hub недоступен напрямую"
    if [ -e /etc/docker/daemon.json ]; then
        echo "   на сервере уже есть /etc/docker/daemon.json — добавьте в него зеркало сами:" >&2
        echo '     "registry-mirrors": ["https://mirror.gcr.io"]' >&2
        echo "   затем: systemctl restart docker и повторите разворачивание" >&2
        exit 1
    fi
    echo "   включаю зеркало mirror.gcr.io в /etc/docker/daemon.json"
    mkdir -p /etc/docker
    printf '{\n  "registry-mirrors": ["https://mirror.gcr.io"]\n}\n' > /etc/docker/daemon.json
    systemctl restart docker
    sleep 3
    timeout 300 docker pull -q golang:1.24-alpine >/dev/null
fi
timeout 300 docker pull -q alpine:3.20 >/dev/null

echo "   $(docker --version)"
echo "   $(docker compose version)"
REMOTE

# По одному токену в MAX может работать только одна копия бота: локальную останавливаем,
# иначе серверная не будет получать сообщения.
if [ -n "$(docker ps -q --filter name=^maxexport$ 2>/dev/null || true)" ]; then
    echo "   останавливаю локальную копию бота (по одному токену работает только одна)"
    docker compose down >/dev/null 2>&1 || true
fi

echo "== 5/6 Сборка и запуск"
ssh "${SSH[@]}" "$SERVER" "cd $DIR && tar xf app.tar && rm app.tar && chmod 600 .env && docker compose up -d --build"

echo "== 6/6 Проверка"
ssh "${SSH[@]}" "$SERVER" "sleep 5; cd $DIR && docker compose ps --format 'table {{.Name}}\t{{.Status}}' && docker compose logs --no-log-prefix --tail 5"
echo
echo "Готово. Бот: https://max.ru/t747_hakaton_max_bot"
echo "Журнал:    ssh $SERVER 'cd $DIR && docker compose logs -f'"
echo "Остановка: ssh $SERVER 'cd $DIR && docker compose down'"
