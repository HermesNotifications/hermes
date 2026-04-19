package bootstrap

import (
	"context"
	"log/slog"
	"os"

	"github.com/hermes-notifications/hermes/internal/cache"
	"github.com/hermes-notifications/hermes/internal/database"
	"github.com/hermes-notifications/hermes/internal/messaging"
	"github.com/hermes-notifications/hermes/internal/observability"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewLogger creates a structured JSON logger writing to stdout.
// The handler is wrapped with observability.TraceHandler so every record
// picks up trace_id/span_id when the context carries an active span.
func NewLogger() *slog.Logger {
	return slog.New(observability.WrapJSONHandler(slog.NewJSONHandler(os.Stdout, nil)))
}

// MustConnectDB connects to Postgres or exits the process.
func MustConnectDB(ctx context.Context, url string, logger *slog.Logger) *pgxpool.Pool {
	pool, err := database.NewPool(ctx, url)
	if err != nil {
		logger.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	return pool
}

// MustConnectNATS connects to NATS or exits the process.
func MustConnectNATS(url string, logger *slog.Logger) *messaging.Client {
	client, err := messaging.Connect(url)
	if err != nil {
		logger.Error("nats connection failed", "error", err)
		os.Exit(1)
	}
	return client
}

// MustSetupStreams creates JetStream streams or exits the process.
func MustSetupStreams(ctx context.Context, client *messaging.Client, logger *slog.Logger) {
	if err := client.SetupStreams(ctx); err != nil {
		logger.Error("nats stream setup failed", "error", err)
		os.Exit(1)
	}
}

// MustConnectRedis connects to Redis or exits the process.
func MustConnectRedis(url string, logger *slog.Logger) *cache.Client {
	client, err := cache.Connect(url)
	if err != nil {
		logger.Error("redis connection failed", "error", err)
		os.Exit(1)
	}
	return client
}
