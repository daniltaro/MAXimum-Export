// Package config читает настройки из переменных окружения и файла .env.
// Все параметры с описанием — в .env.example в корне проекта.
package config

import (
	"bufio"
	"os"
	"strings"
)

// Config — настройки программы.
type Config struct {
	Host           string // адрес, на котором слушает HTTP-сервер (0.0.0.0 — доступен из локальной сети и Docker)
	Port           string // порт HTTP-сервера
	RateSource     string // training — учебный курс (по умолчанию), cbr — курс ЦБ РФ
	AllowedOrigins string // для CORS: адреса фронтенда через запятую или "*"
	BotToken       string // токен бота MAX (этап 4); пусто — бот не запускается
	TrustProxy     bool   // true — сервер за обратным прокси (Caddy/nginx): адрес клиента из X-Forwarded-For
}

// Load читает .env (если он есть) и переменные окружения. Переменные окружения важнее .env.
func Load() Config {
	loadDotEnv(".env")
	return Config{
		Host:           get("HOST", "0.0.0.0"),
		Port:           get("PORT", "8080"),
		RateSource:     get("RATE_SOURCE", "training"),
		AllowedOrigins: get("ALLOWED_ORIGINS", "*"),
		BotToken:       get("BOT_TOKEN", ""),
		TrustProxy:     get("TRUST_PROXY", "false") == "true",
	}
}

func get(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// loadDotEnv читает строки КЛЮЧ=значение; пустые строки и # комментарии пропускает.
// Уже заданные переменные окружения не перезаписывает.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return // файла нет — это нормально (например, в Docker переменные задаются в compose.yaml)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, val = strings.TrimSpace(key), strings.Trim(strings.TrimSpace(val), `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, val)
		}
	}
}
