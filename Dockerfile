# Сборка: собираем одну программу cmd/app (чат-бот MAX + HTTP API мини-приложения).
# Справочники, тексты и описание API вшиты в программу, отдельных файлов на сервере не нужно.
FROM golang:1.24-alpine AS build
WORKDIR /src

# Сначала только зависимости: пока go.mod и go.sum не меняются, Docker берёт их из кэша.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/maxexport ./cmd/app

# Запуск.
FROM alpine:3.20

# Серверы MAX используют сертификат Минцифры («Russian Trusted Root CA»), которого нет
# в списке доверенных ни в Alpine, ни в macOS. Без него бот не подключится к MAX:
# «certificate signed by unknown authority». Сертификат ставится ТОЛЬКО внутрь контейнера
# и на систему разработчика не влияет. Источник — официальный сайт Минцифры gu-st.ru.
# Отпечаток сертификата проверяется: если по адресу окажется другой файл, сборка упадёт.
ARG RUSSIAN_ROOT_CA_URL=https://gu-st.ru/content/lending/russian_trusted_root_ca_pem.crt
ARG RUSSIAN_ROOT_CA_SHA256=D26D2D0231B7C39F92CC738512BA54103519E4405D68B5BD703E9788CA8ECF31
RUN apk add --no-cache ca-certificates openssl \
    && wget -qO /usr/local/share/ca-certificates/russian_trusted_root_ca.crt "$RUSSIAN_ROOT_CA_URL" \
    && openssl x509 -in /usr/local/share/ca-certificates/russian_trusted_root_ca.crt -noout -subject \
       | grep -q "Russian Trusted Root CA" \
    && [ "$(openssl x509 -in /usr/local/share/ca-certificates/russian_trusted_root_ca.crt -noout -fingerprint -sha256 \
       | sed 's/.*=//; s/://g')" = "$RUSSIAN_ROOT_CA_SHA256" ] \
    && update-ca-certificates \
    && apk del openssl

# Программа работает от обычного пользователя, а не от root.
RUN adduser -D -H -u 10001 maxexport
COPY --from=build /out/maxexport /usr/local/bin/maxexport
USER maxexport

EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
    CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/maxexport"]
