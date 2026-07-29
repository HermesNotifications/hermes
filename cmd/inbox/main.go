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
	"github.com/hermes-notifications/hermes/internal/messaging"
	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/hermes-notifications/hermes/internal/store/dynamo"
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
	defer natsClient.Close()

	redisClient := bootstrap.MustConnectRedis(cfg.RedisURL, logger)
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

	srv := inbox.NewServer(inboxStore, centrifugoClient, natsClient, redisClient, keyProvider, logger)

	bootstrap.ListenAndServe(fmt.Sprintf(":%d", cfg.HTTPPort), srv.Handler(), logger)
}

// inboxStoreWithDynamoInbox delegates InboxRepository methods to DynamoDB while
// routing GetCategoryByID (needed for slug resolution) to the Postgres store.
type inboxStoreWithDynamoInbox struct {
	*postgres.Store
	notifs *dynamo.NotificationStore
}

func (s *inboxStoreWithDynamoInbox) ListInbox(ctx context.Context, userID string, archived bool, cursor string, limit int) ([]models.Notification, int, string, error) {
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
