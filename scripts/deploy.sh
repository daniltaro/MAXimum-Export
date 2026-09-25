#!/usr/bin/env bash
# Разворачивание MAXimum Export на сервер одной командой.
#
#   ./scripts/deploy.sh root@81.200.0.1
#
# Что делает:
#   1) проверяет, что локально всё собирается и тесты проходят;
#   2) копирует на сервер текущую версию из git (без .git, секретов и мусора);
#   3) копирует .env с токеном бота — по шифрованному каналу, в репозиторий он не попадает;
#   4) ставит Docker, если его нет;
#   5) собирает образ и запускает бота; после перезагрузки сервера он поднимется сам;
#   6) проверяет, что бот подключился к MAX.
set -euo pipefail

SERVER="${1:-}"
DIR="${2:-/opt/maxexport}"
if [ -z "$SERVER" ]; then
    echo "Использование: $0 пользователь@адрес-сервера [папка на сервере]" >&2
    exit 1
fi

cd "$(dirname "$0")/.."

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
trap 'rm -rf "$TMP"' EXIT
git archive --format=tar HEAD -o "$TMP/app.tar"

echo "== 3/6 Копирование на сервер ($SERVER:$DIR)"
ssh "$SERVER" "mkdir -p $DIR"
scp -q "$TMP/app.tar" "$SERVER:$DIR/app.tar"
scp -q .env "$SERVER:$DIR/.env"

echo "== 4/6 Установка Docker, если его нет"
ssh "$SERVER" 'command -v docker >/dev/null || (curl -fsSL https://get.docker.com | sh)'

echo "== 5/6 Сборка и запуск"
ssh "$SERVER" "cd $DIR && tar xf app.tar && rm app.tar && chmod 600 .env && docker compose up -d --build"

echo "== 6/6 Проверка"
sleep 5
ssh "$SERVER" "cd $DIR && docker compose ps --format 'table {{.Name}}\t{{.Status}}' && docker compose logs --no-log-prefix --tail 5"
echo
echo "Готово. Бот: https://max.ru/t747_hakaton_max_bot"
echo "Журнал:   ssh $SERVER 'cd $DIR && docker compose logs -f'"
echo "Остановка: ssh $SERVER 'cd $DIR && docker compose down'"
