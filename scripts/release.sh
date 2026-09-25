#!/usr/bin/env bash
# Фиксация версии решения: архив коммита и контрольная сумма SHA-256.
#
#   ./scripts/release.sh
#
# Хеш коммита и файл .sha256 — это «зафиксированная версия исходного кода» для жюри:
# архив собирается из коммита, поэтому его содержимое нельзя изменить незаметно.
set -euo pipefail

cd "$(dirname "$0")/.."

if [ -n "$(git status --porcelain)" ]; then
    echo "Есть незакоммиченные изменения — сначала зафиксируйте их в git:" >&2
    git status --short >&2
    exit 1
fi

echo "== Проверка проекта"
go vet ./...
go test ./...

HASH="$(git rev-parse --short HEAD)"
NAME="maximum-export-$HASH.tar.gz"
mkdir -p dist
git archive --format=tar.gz --prefix="maximum-export-$HASH/" -o "dist/$NAME" HEAD

# Сумма считается от корня проекта, с путём dist/... — тогда проверка запускается
# оттуда же, откуда собирали. shasum есть в macOS, sha256sum — в Linux.
if command -v shasum >/dev/null; then
    shasum -a 256 "dist/$NAME" > "dist/$NAME.sha256"
else
    sha256sum "dist/$NAME" > "dist/$NAME.sha256"
fi

echo
echo "Коммит:          $(git rev-parse HEAD)"
echo "Архив:           dist/$NAME ($(du -h "dist/$NAME" | cut -f1))"
echo "Контрольная сумма: $(cut -d' ' -f1 < "dist/$NAME.sha256")"
echo
echo "Проверка архива: shasum -a 256 -c dist/$NAME.sha256"
