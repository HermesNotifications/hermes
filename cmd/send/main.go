package main

import (
	"context"
	"fmt"
	"time"

	"github.com/hermes-notifications/hermes/internal/bootstrap"
	"github.com/hermes-notifications/hermes/internal/config"
	"github.com/hermes-notifications/hermes/internal/messaging"
	"github.com/hermes-notifications/hermes/internal/send"
	"github.com/hermes-notifications/hermes/internal/store/postgres"
)

func main() {
	logger := bootstrap.NewLogger()
	cfg := config.MustLoad()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool := bootstrap.MustConnectDB(ctx, cfg.DatabaseURL, logger)
	defer pool.Close()

	natsClient := bootstrap.MustConnectNATS(cfg.NATSUrl, logger, messaging.WithCABundle(cfg.NATSCABundlePath))
	bootstrap.MustSetupStreams(ctx, natsClient, logger)
	defer natsClient.Close()

	redisClient := bootstrap.MustConnectRedis(cfg.RedisURL, logger)
	defer redisClient.Close()

	st := postgres.New(pool)

	srv := send.NewServer(st, natsClient, redisClient, pool, cfg.APIKeyHMACSecret, logger)

	bootstrap.ListenAndServe(fmt.Sprintf(":%d", cfg.HTTPPort), srv.Handler(), logger)
}
