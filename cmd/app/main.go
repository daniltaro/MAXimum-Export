// Программа MAXimum Export: чат-бот MAX и HTTP API для мини-приложения в одной программе.
//
//	go run ./cmd/app          # API: http://localhost:8080/api/v1/meta
//	                          # бот MAX запускается, если в .env задан BOT_TOKEN
package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"maxexport/data"
	"maxexport/internal/api"
	"maxexport/internal/config"
	"maxexport/internal/engine"
	"maxexport/internal/flow"
	"maxexport/internal/maxbot"
	"maxexport/internal/service"
)

func main() {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Курс валют: учебный по умолчанию; курс ЦБ обновляется в фоне раз в час,
	// поэтому ответ пользователю никогда не ждёт сайт ЦБ (ТЗ §21: ответ ≤ 5 секунд).
	var rates engine.RateSource = engine.TrainingSource{}
	if cfg.RateSource == engine.SourceCBR {
		cbr := engine.NewCBRSource()
		go cbr.Run(ctx, time.Hour)
		rates = cbr
		log.Println("курс валют: ЦБ РФ (обновление раз в час)")
	} else {
		log.Println("курс валют: учебный (1 € = 100 ₽)")
	}

	svc := service.New(data.MustLoad(), rates)

	// Чат-бот MAX: получает сообщения через long polling — белый IP и домен не нужны.
	botDone := make(chan struct{})
	close(botDone) // бот не запущен — ждать нечего
	if cfg.BotToken != "" {
		bot := flow.New(svc)
		adapter, err := maxbot.New(cfg.BotToken, bot)
		if err != nil {
			log.Fatalf("MAX: %v", err)
		}
		botDone = make(chan struct{})
		go func() {
			defer close(botDone)
			if err := adapter.Run(ctx); err != nil {
				log.Printf("MAX: бот остановлен: %v", err)
			}
		}()
	} else {
		log.Println("MAX: BOT_TOKEN не задан — бот не запущен, работает только API")
	}
	srv := &http.Server{
		Addr:              net.JoinHostPort(cfg.Host, cfg.Port),
		Handler:           api.New(svc, api.Options{AllowedOrigins: cfg.AllowedOrigins, TrustProxy: cfg.TrustProxy}).Handler(),
		MaxHeaderBytes:    16 << 10, // 16 КБ: длинные адреса не нужны ни одному запросу API
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Printf("API: http://localhost:%s/api/v1/meta (описание — /api/v1/openapi.yaml)", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP-сервер: %v", err)
		}
	}()

	<-ctx.Done() // Ctrl+C или docker stop
	log.Println("остановка…")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	<-botDone // даём боту дописать начатые ответы пользователям
	log.Println("остановлено")
}
