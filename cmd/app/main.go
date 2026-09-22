// Программа MAXimum Export: HTTP API для мини-приложения (этап 1.5).
// На следующих этапах здесь же запускаются чат-бот MAX и файлы мини-приложения.
//
//	go run ./cmd/app          # http://localhost:8080/api/v1/meta
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
	srv := &http.Server{
		Addr:              net.JoinHostPort(cfg.Host, cfg.Port),
		Handler:           api.New(svc, cfg.AllowedOrigins).Handler(),
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
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
