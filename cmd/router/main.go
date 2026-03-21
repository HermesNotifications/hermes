package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hermes-notifications/hermes/internal/cache"
	"github.com/hermes-notifications/hermes/internal/config"
	"github.com/hermes-notifications/hermes/internal/database"
	"github.com/hermes-notifications/hermes/internal/messaging"
	"github.com/hermes-notifications/hermes/internal/router"
	"github.com/hermes-notifications/hermes/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config.Load()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	natsClient, err := messaging.Connect(cfg.NATSUrl)
	if err != nil {
		logger.Error("nats connection failed", "error", err)
		os.Exit(1)
	}
	defer natsClient.Close()

	if err := natsClient.SetupStreams(ctx); err != nil {
		logger.Error("nats stream setup failed", "error", err)
		os.Exit(1)
	}

	redisClient, err := cache.Connect(cfg.RedisURL)
	if err != nil {
		logger.Error("redis connection failed", "error", err)
		os.Exit(1)
	}
	defer redisClient.Close()

	st := store.New(pool)
	templateResolver := router.NewTemplateResolver(st, redisClient)
	channelResolver := router.NewChannelResolver(st)
	r := router.NewRouter(natsClient, st, templateResolver, channelResolver, logger)

	if err := r.Start(); err != nil {
		logger.Error("router start failed", "error", err)
		os.Exit(1)
	}

	// Health check server
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	httpServer := &http.Server{
		Addr:    ":8081",
		Handler: mux,
	}

	go func() {
		logger.Info("router service starting", "health_port", 8081)
		if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
			logger.Error("http server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	httpServer.Shutdown(shutdownCtx)
}
