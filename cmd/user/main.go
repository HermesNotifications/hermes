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
	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/hermes-notifications/hermes/internal/store/dynamo"
	"github.com/hermes-notifications/hermes/internal/store/postgres"
	"github.com/hermes-notifications/hermes/internal/userservice"
)

func main() {
	logger := bootstrap.NewLogger()
	cfg := config.MustLoad()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool := bootstrap.MustConnectDB(ctx, cfg.DatabaseURL, logger)
	defer pool.Close()

	redisClient := bootstrap.MustConnectRedis(cfg.RedisURL, logger)
	defer redisClient.Close()

	pgStore := postgres.New(pool)

	// Ensure Hermes internal signing key exists
	if err := pgStore.EnsureHermesSigningKey(ctx, cfg.JWTSecret); err != nil {
		logger.Error("failed to ensure hermes signing key", "error", err)
		os.Exit(1)
	}

	cachedKeys := auth.NewCachedKeyProvider(func() []auth.JWTSigningConfig {
		keys, err := pgStore.ListActiveJWTSigningKeys(context.Background())
		if err != nil {
			logger.Error("failed to load JWT signing keys", "error", err)
			return nil
		}
		return auth.SigningConfigsFromKeys(keys)
	}, time.Minute, redisClient)
	keyProvider := cachedKeys.Provider()

	var userStore userservice.UserStore = pgStore
	if cfg.DynamoEndpoint != "" {
		dynamoClient := bootstrap.MustConnectDynamo(ctx, cfg.DynamoEndpoint, cfg.DynamoRegion, cfg.EventRetentionDays, logger)
		userStore = &userStoreWithDynamoSubs{
			Store: pgStore,
			subs:  dynamo.NewUserSubscriptionStore(dynamoClient),
		}
	}

	srv := userservice.NewServer(userStore, keyProvider, logger)
	srv.ConfigureRateLimit(cfg.RateLimitEnabled, cfg.RateLimitBurst, cfg.RateLimitPerSecond)

	bootstrap.ListenAndServe(fmt.Sprintf(":%d", cfg.HTTPPort), srv.Handler(), logger)
}

// userStoreWithDynamoSubs delegates user subscription operations to DynamoDB
// while routing all other UserStore methods to the Postgres store.
// The embedded *postgres.Store satisfies the non-subscription methods; the
// explicit methods below shadow the embedded ones for subscriptions.
type userStoreWithDynamoSubs struct {
	*postgres.Store
	subs *dynamo.UserSubscriptionStore
}

func (u *userStoreWithDynamoSubs) GetUserSubscriptions(ctx context.Context, userID string) ([]models.UserSubscription, error) {
	return u.subs.GetUserSubscriptions(ctx, userID)
}

func (u *userStoreWithDynamoSubs) SetUserSubscription(ctx context.Context, userID, subscriptionID string, optedIn bool) (*models.UserSubscription, error) {
	return u.subs.SetUserSubscription(ctx, userID, subscriptionID, optedIn)
}

func (u *userStoreWithDynamoSubs) DeleteUserSubscription(ctx context.Context, userID, subscriptionID string) error {
	return u.subs.DeleteUserSubscription(ctx, userID, subscriptionID)
}
