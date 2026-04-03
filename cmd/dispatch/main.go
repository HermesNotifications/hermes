package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/hermes-notifications/hermes/internal/bootstrap"
	"github.com/hermes-notifications/hermes/internal/config"
	"github.com/hermes-notifications/hermes/internal/dispatch"
	"github.com/hermes-notifications/hermes/internal/httputil"
	"github.com/hermes-notifications/hermes/internal/store/cached"
	"github.com/hermes-notifications/hermes/internal/store/postgres"
)

func main() {
	logger := bootstrap.NewLogger()
	cfg := config.Load()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool := bootstrap.MustConnectDB(ctx, cfg.DatabaseURL, logger)
	defer pool.Close()

	natsClient := bootstrap.MustConnectNATS(cfg.NATSUrl, logger)
	bootstrap.MustSetupStreams(ctx, natsClient, logger)
	defer natsClient.Close()

	redisClient := bootstrap.MustConnectRedis(cfg.RedisURL, logger)
	defer redisClient.Close()

	st := postgres.New(pool)
	tenants := cached.NewTenantRepository(st, redisClient)
	templateResolver := dispatch.NewTemplateResolver(st, redisClient)
	channelResolver := dispatch.NewChannelResolver(st, redisClient)
	d := dispatch.NewDispatch(natsClient, st, st, tenants, templateResolver, channelResolver, logger)

	if err := d.Start(); err != nil {
		logger.Error("dispatch start failed", "error", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", httputil.HealthzHandler())
	mux.HandleFunc("GET /readyz", httputil.ReadyzHandler())

	bootstrap.ListenAndServe(fmt.Sprintf(":%d", cfg.HTTPPort), mux, logger)
}
