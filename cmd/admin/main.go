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
	cfg := config.Load()

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

	tenants := cached.NewTenantRepository(pgStore, redisClient)

	var adminStore admin.AdminStore = pgStore
	if cfg.DynamoEndpoint != "" {
		dynamoClient := bootstrap.MustConnectDynamo(ctx, cfg.DynamoEndpoint, cfg.DynamoRegion, logger)
		adminStore = &adminStoreWithDynamoEvents{
			Store:  pgStore,
			events: dynamo.NewEventStore(dynamoClient, pgStore),
		}
	}

	srv := admin.NewServer(adminStore, tenants, redisClient, pool, []byte(cfg.JWTSecret), cfg.APIKeyHMACSecret, logger)

	bootstrap.ListenAndServe(fmt.Sprintf(":%d", cfg.HTTPPort), srv.Handler(), logger)
}

// adminStoreWithDynamoEvents delegates GetNotificationEvents to DynamoDB so the
// admin notification detail view reads from the same store events are written to.
// All other AdminStore methods are handled by the embedded *postgres.Store.
type adminStoreWithDynamoEvents struct {
	*postgres.Store
	events *dynamo.EventStore
}

func (a *adminStoreWithDynamoEvents) GetNotificationEvents(ctx context.Context, notificationID string) ([]models.NotificationEvent, error) {
	return a.events.GetNotificationEvents(ctx, notificationID)
}
