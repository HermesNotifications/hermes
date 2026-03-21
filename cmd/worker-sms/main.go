package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hermes-notifications/hermes/internal/config"
	"github.com/hermes-notifications/hermes/internal/delivery"
	"github.com/hermes-notifications/hermes/internal/messaging"
)

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config.Load()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	natsClient, err := messaging.Connect(cfg.NATSUrl)
	if err != nil {
		logger.Error("nats", "error", err)
		os.Exit(1)
	}
	defer natsClient.Close()

	if err := natsClient.SetupStreams(ctx); err != nil {
		logger.Error("nats stream setup", "error", err)
		os.Exit(1)
	}

	webhookURL := envStr("HERMES_SMS_WEBHOOK_URL", "http://localhost:9090/sms")
	provider := delivery.NewWebhookProvider("sms", webhookURL)

	worker := delivery.NewWorker(natsClient, provider, "sms", "worker-sms", logger)
	if err := worker.Start(context.Background()); err != nil {
		logger.Error("start worker", "error", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	go http.ListenAndServe(":8084", mux)

	logger.Info("worker-sms started")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down")
}
