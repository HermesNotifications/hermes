// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

// Package dynamo provides store implementations backed by DynamoDB (or any
// DynamoDB-compatible API, including ExtendDB).
//
// When HERMES_DYNAMO_ENDPOINT is set the client is pointed at that URL with
// static fake credentials — suitable for local dev via ExtendDB or for CI
// pipelines. When it is empty the standard AWS SDK credential chain is used
// (IAM roles, environment variables, ~/.aws/credentials), connecting to real
// DynamoDB on AWS.
//
// Table names are configurable to support multi-app or multi-stage deployments;
// the defaults match the documented single-table design in docs/adr/0001-*.
package dynamo

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const (
	defaultEventsTable            = "hermes-events"
	defaultUserSubscriptionsTable = "hermes-user-subscriptions"
	defaultNotificationsTable     = "hermes-notifications"

	// ttlAttr is the DynamoDB TTL attribute name — must match the TTL attribute
	// configured on the table via UpdateTimeToLive.
	ttlAttr = "ttl"

	// GSI names for hermes-notifications.
	gsiByUser          = "gsi-by-user"          // PK=user_id, SK=notif_id — inbox listing
	gsiByIdempotency   = "gsi-by-idempotency"   // PK=idem_pk, SK=created_at — dispatch dedup
)

// Client wraps a DynamoDB client with table-name and retention configuration.
type Client struct {
	db                     *dynamodb.Client
	EventsTable            string
	UserSubscriptionsTable string
	NotificationsTable     string
	// RetentionDays is the number of days before event items are expired via DynamoDB TTL.
	// Defaults to 90. Set via bootstrap.MustConnectDynamo from cfg.EventRetentionDays.
	RetentionDays int
}

// NewClient constructs a Client. If endpoint is non-empty the client is
// pointed at that URL with static credentials (ExtendDB / local dev mode).
// Otherwise the AWS SDK default credential chain is used.
func NewClient(ctx context.Context, endpoint, region string) (*Client, error) {
	var opts []func(*awsconfig.LoadOptions) error
	opts = append(opts, awsconfig.WithRegion(region))

	if endpoint != "" {
		// Local dev / CI (DynamoDB Local) and multi-cloud production (ExtendDB):
		// use static credentials and a custom endpoint. DynamoDB Local accepts any
		// credentials; ExtendDB requires real IAM users created via `extenddb manage`.
		// For ExtendDB in production, inject real keys via HERMES_DYNAMO_ACCESS_KEY_ID
		// / HERMES_DYNAMO_SECRET_ACCESS_KEY (TODO: wire when ExtendDB is deployed).
		opts = append(opts,
			awsconfig.WithCredentialsProvider(
				credentials.NewStaticCredentialsProvider("local", "local", ""),
			),
			awsconfig.WithBaseEndpoint(endpoint),
		)
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, err
	}

	db := dynamodb.NewFromConfig(cfg)
	return &Client{
		db:                     db,
		EventsTable:            defaultEventsTable,
		UserSubscriptionsTable: defaultUserSubscriptionsTable,
		NotificationsTable:     defaultNotificationsTable,
		RetentionDays:          90,
	}, nil
}

// EnsureTables creates the DynamoDB tables if they do not already exist, and
// activates native TTL on the events table so the `ttl` attribute is honoured.
// Idempotent — safe to call on every startup. In production (real DynamoDB)
// tables should be pre-provisioned via IaC; this helper targets local/ExtendDB.
func (c *Client) EnsureTables(ctx context.Context) error {
	if err := c.ensureTable(ctx, c.EventsTable, "pk", "sk"); err != nil {
		return err
	}
	if err := c.enableTTL(ctx, c.EventsTable, ttlAttr); err != nil {
		return err
	}
	if err := c.ensureTable(ctx, c.UserSubscriptionsTable, "pk", "sk"); err != nil {
		return err
	}
	return c.ensureNotificationsTable(ctx)
}

// enableTTL activates DynamoDB native TTL on the given table/attribute if not already enabled.
// Idempotent: skips the API call when the TTL spec is already ENABLED or ENABLING.
// DynamoDB Local may return a ValidationException for TTL operations; that is swallowed.
func (c *Client) enableTTL(ctx context.Context, tableName, attrName string) error {
	desc, err := c.db.DescribeTimeToLive(ctx, &dynamodb.DescribeTimeToLiveInput{
		TableName: aws.String(tableName),
	})
	if err != nil {
		// DynamoDB Local does not fully support DescribeTimeToLive — treat as not-yet-enabled.
		// Fall through to UpdateTimeToLive which will also be a no-op or harmless error.
		desc = nil
	}
	if desc != nil && desc.TimeToLiveDescription != nil {
		s := desc.TimeToLiveDescription.TimeToLiveStatus
		if s == types.TimeToLiveStatusEnabled || s == types.TimeToLiveStatusEnabling {
			return nil // already active
		}
	}

	_, err = c.db.UpdateTimeToLive(ctx, &dynamodb.UpdateTimeToLiveInput{
		TableName: aws.String(tableName),
		TimeToLiveSpecification: &types.TimeToLiveSpecification{
			Enabled:       aws.Bool(true),
			AttributeName: aws.String(attrName),
		},
	})
	if err != nil {
		// Enabling TTL is best-effort. The well-known cases below are non-fatal and
		// must not block startup — checked via the smithy APIError interface so we
		// don't import the smithy package directly (it is already an indirect dep):
		//   ValidationException     — DynamoDB Local lacks TTL support, or TTL is already enabling
		//   AccessDeniedException   — pre-provisioned (IaC) table where the app role has only
		//                             data-plane permissions; TTL is managed out-of-band
		//   ResourceNotFoundException — table managed elsewhere; nothing for us to configure
		type apiErr interface{ ErrorCode() string }
		var ae apiErr
		if errors.As(err, &ae) {
			switch ae.ErrorCode() {
			case "ValidationException", "AccessDeniedException", "ResourceNotFoundException":
				return nil
			}
		}
		return fmt.Errorf("enable TTL on %s: %w", tableName, err)
	}
	return nil
}

// ensureNotificationsTable creates hermes-notifications with its two GSIs.
// gsi-by-user  (PK=user_id, SK=notif_id)   — inbox listing sorted by time
// gsi-by-idempotency (PK=idem_pk, SK=created_at) — dispatch dedup within 24h window
func (c *Client) ensureNotificationsTable(ctx context.Context) error {
	_, err := c.db.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: aws.String(c.NotificationsTable),
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("pk"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("sk"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("user_id"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("notif_id"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("idem_pk"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("created_at"), AttributeType: types.ScalarAttributeTypeS},
		},
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String("pk"), KeyType: types.KeyTypeHash},
			{AttributeName: aws.String("sk"), KeyType: types.KeyTypeRange},
		},
		GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{
			{
				IndexName: aws.String(gsiByUser),
				KeySchema: []types.KeySchemaElement{
					{AttributeName: aws.String("user_id"), KeyType: types.KeyTypeHash},
					{AttributeName: aws.String("notif_id"), KeyType: types.KeyTypeRange},
				},
				Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll},
			},
			{
				IndexName: aws.String(gsiByIdempotency),
				KeySchema: []types.KeySchemaElement{
					{AttributeName: aws.String("idem_pk"), KeyType: types.KeyTypeHash},
					{AttributeName: aws.String("created_at"), KeyType: types.KeyTypeRange},
				},
				Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll},
			},
		},
		BillingMode: types.BillingModePayPerRequest,
	})
	if err != nil {
		var riue *types.ResourceInUseException
		if errors.As(err, &riue) {
			return nil
		}
		return err
	}
	return nil
}

func (c *Client) ensureTable(ctx context.Context, name, pkAttr, skAttr string) error {
	_, err := c.db.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: aws.String(name),
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String(pkAttr), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String(skAttr), AttributeType: types.ScalarAttributeTypeS},
		},
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String(pkAttr), KeyType: types.KeyTypeHash},
			{AttributeName: aws.String(skAttr), KeyType: types.KeyTypeRange},
		},
		BillingMode: types.BillingModePayPerRequest,
	})
	if err != nil {
		// ResourceInUseException means the table already exists — not an error.
		var riue *types.ResourceInUseException
		if errors.As(err, &riue) {
			return nil
		}
		return err
	}
	return nil
}
