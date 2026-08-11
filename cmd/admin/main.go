// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/hermes-notifications/hermes/internal/admin"
	"github.com/hermes-notifications/hermes/internal/bootstrap"
	"github.com/hermes-notifications/hermes/internal/config"
	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/hermes-notifications/hermes/internal/store/cached"
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

	redisClient := bootstrap.MustConnectRedis(cfg.RedisURL, logger)
	defer redisClient.Close()

	pgStore := postgres.New(pool)

	// Ensure Hermes internal signing key exists
	if err := pgStore.EnsureHermesSigningKey(ctx, cfg.JWTSecret); err != nil {
		logger.Error("failed to ensure hermes signing key", "error", err)
		os.Exit(1)
	}

	organizations := cached.NewOrganizationRepository(pgStore, redisClient)

	var adminStore admin.AdminStore = pgStore
	if cfg.DynamoEndpoint != "" {
		dynamoClient := bootstrap.MustConnectDynamo(ctx, cfg.DynamoEndpoint, cfg.DynamoRegion, cfg.EventRetentionDays, logger)
		evStore := dynamo.NewEventStore(dynamoClient, pgStore)
		notifStore := dynamo.NewNotificationStore(dynamoClient, evStore)
		adminStore = &adminStoreWithDynamoNotifs{
			Store:  pgStore,
			notifs: notifStore,
		}
	}

	srv := admin.NewServer(adminStore, organizations, redisClient, pool, []byte(cfg.JWTSecret), cfg.APIKeyHMACSecret, logger)
	srv.ConfigureRateLimit(cfg.RateLimitEnabled, cfg.RateLimitBurst, cfg.RateLimitPerSecond)

	bootstrap.ListenAndServe(fmt.Sprintf(":%d", cfg.HTTPPort), srv.Handler(), logger)
}

// adminStoreWithDynamoNotifs routes GetNotificationByID and GetNotificationEvents
// to DynamoDB while delegating ListRecentNotifications (cross-organization admin scan)
// and all other AdminStore methods to the Postgres store.
type adminStoreWithDynamoNotifs struct {
	*postgres.Store
	notifs *dynamo.NotificationStore
}

func (a *adminStoreWithDynamoNotifs) GetNotificationByID(ctx context.Context, id string) (*models.Notification, error) {
	return a.notifs.GetNotificationByID(ctx, id)
}

func (a *adminStoreWithDynamoNotifs) GetNotificationEvents(ctx context.Context, notificationID string) ([]models.NotificationEvent, error) {
	return a.notifs.GetNotificationEvents(ctx, notificationID)
}
