// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/bootstrap"
	"github.com/hermes-notifications/hermes/internal/config"
	"github.com/hermes-notifications/hermes/internal/store/postgres"
	"github.com/hermes-notifications/hermes/internal/userservice"
)

func main() {
	logger := bootstrap.NewLogger()
	cfg := config.Load()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool := bootstrap.MustConnectDB(ctx, cfg.DatabaseURL, logger)
	defer pool.Close()

	redisClient := bootstrap.MustConnectRedis(cfg.RedisURL, logger)
	defer redisClient.Close()

	st := postgres.New(pool)

	// Ensure Hermes internal signing key exists
	if err := st.EnsureHermesSigningKey(ctx, cfg.JWTSecret); err != nil {
		logger.Error("failed to ensure hermes signing key", "error", err)
		os.Exit(1)
	}

	cachedKeys := auth.NewCachedKeyProvider(func() []auth.JWTSigningConfig {
		keys, err := st.ListActiveJWTSigningKeys(context.Background())
		if err != nil {
			logger.Error("failed to load JWT signing keys", "error", err)
			return nil
		}
		return auth.SigningConfigsFromKeys(keys)
	}, time.Minute, redisClient)
	keyProvider := cachedKeys.Provider()

	srv := userservice.NewServer(st, keyProvider, logger)

	bootstrap.ListenAndServe(fmt.Sprintf(":%d", cfg.HTTPPort), srv.Handler(), logger)
}
