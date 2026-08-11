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
	"github.com/hermes-notifications/hermes/internal/centrifugo"
	"github.com/hermes-notifications/hermes/internal/config"
	"github.com/hermes-notifications/hermes/internal/inbox"
	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/hermes-notifications/hermes/internal/store/dynamo"
	"github.com/hermes-notifications/hermes/internal/store/postgres"
)

func main() {
	logger := bootstrap.NewLogger()
	cfg := config.MustLoad()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool := bootstrap.MustConnectDB(ctx, cfg, logger)
	defer pool.Close()

	// No NATS connection here on purpose. This service reads from the store and pushes
	// nothing onto the bus; it used to connect and hand the client to a field nobody read.
	// Under ADR 0005 phase 3 that dead connection would have needed its own NKey user and
	// permissions, so it is gone instead.
	redisClient := bootstrap.MustConnectRedis(cfg, logger)
	defer redisClient.Close()

	centrifugoClient := centrifugo.NewClient(cfg.CentrifugoAPIURL, cfg.CentrifugoAPIKey)

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

	var inboxStore inbox.InboxStore = pgStore
	if cfg.DynamoEndpoint != "" {
		dynamoClient := bootstrap.MustConnectDynamo(ctx, cfg.DynamoEndpoint, cfg.DynamoRegion, cfg.EventRetentionDays, logger)
		evStore := dynamo.NewEventStore(dynamoClient, pgStore)
		notifStore := dynamo.NewNotificationStore(dynamoClient, evStore)
		inboxStore = &inboxStoreWithDynamoInbox{
			Store:  pgStore,
			notifs: notifStore,
		}
	}

	srv := inbox.NewServer(inboxStore, centrifugoClient, redisClient, keyProvider, logger)
	srv.ConfigureRateLimit(cfg.RateLimitEnabled, cfg.RateLimitBurst, cfg.RateLimitPerSecond)

	// Postgres only. Redis is deliberately not a readiness dependency: every read it serves
	// falls back to the database, so a Redis blip must not empty this service's endpoint list.
	readiness := bootstrap.NewReadiness(bootstrap.PostgresCheck(pool))
	srv.SetReadiness(readiness)

	bootstrap.ListenAndServeWithOptions(fmt.Sprintf(":%d", cfg.HTTPPort), srv.Handler(), logger,
		bootstrap.ServeOptions{
			Readiness:       readiness,
			DrainDelay:      cfg.ShutdownDrainDelay,
			ShutdownTimeout: cfg.ShutdownTimeout,
		})
}

// inboxStoreWithDynamoInbox delegates InboxRepository methods to DynamoDB while
// routing GetCategoryByID (needed for slug resolution) to the Postgres store.
type inboxStoreWithDynamoInbox struct {
	*postgres.Store
	notifs *dynamo.NotificationStore
}

func (s *inboxStoreWithDynamoInbox) ListInbox(ctx context.Context, userID string, archived bool, cursor string, limit int) ([]models.Notification, string, error) {
	return s.notifs.ListInbox(ctx, userID, archived, cursor, limit)
}
func (s *inboxStoreWithDynamoInbox) UnreadCount(ctx context.Context, userID string) (int, error) {
	return s.notifs.UnreadCount(ctx, userID)
}
func (s *inboxStoreWithDynamoInbox) MarkRead(ctx context.Context, userID, notificationID string) (bool, error) {
	return s.notifs.MarkRead(ctx, userID, notificationID)
}
func (s *inboxStoreWithDynamoInbox) MarkUnread(ctx context.Context, userID, notificationID string) (bool, error) {
	return s.notifs.MarkUnread(ctx, userID, notificationID)
}
func (s *inboxStoreWithDynamoInbox) Archive(ctx context.Context, userID, notificationID string) (bool, error) {
	return s.notifs.Archive(ctx, userID, notificationID)
}
func (s *inboxStoreWithDynamoInbox) Unarchive(ctx context.Context, userID, notificationID string) (bool, error) {
	return s.notifs.Unarchive(ctx, userID, notificationID)
}
func (s *inboxStoreWithDynamoInbox) SoftDelete(ctx context.Context, userID, notificationID string) (bool, error) {
	return s.notifs.SoftDelete(ctx, userID, notificationID)
}
func (s *inboxStoreWithDynamoInbox) MarkAllRead(ctx context.Context, userID string) error {
	return s.notifs.MarkAllRead(ctx, userID)
}
