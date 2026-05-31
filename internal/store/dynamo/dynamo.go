// Package dynamo provides store implementations backed by DynamoDB (or any
// DynamoDB-compatible API, including ExtendDB).
//
// When HERMES_DYNAMO_ENDPOINT is set the client is pointed at that URL with
// static fake credentials — suitable for local dev via ExtendDB or for CI
// pipelines. When it is empty the standard AWS SDK credential chain is used
// (IAM roles, environment variables, ~/.aws/credentials), connecting to real
// DynamoDB on AWS.
//
// Table names are configurable to support multi-tenant or multi-stage deployments;
// the defaults match the documented single-table design in docs/adr/0001-*.
package dynamo

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const (
	defaultEventsTable            = "hermes-events"
	defaultUserSubscriptionsTable = "hermes-user-subscriptions"

	// ttlAttr is the DynamoDB TTL attribute name — must match the TTL attribute
	// configured on the table via UpdateTimeToLive.
	ttlAttr = "ttl"
)

// Client wraps a DynamoDB client with table-name configuration.
type Client struct {
	db                     *dynamodb.Client
	EventsTable            string
	UserSubscriptionsTable string
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
	}, nil
}

// EnsureTables creates the DynamoDB tables if they do not already exist.
// Idempotent — safe to call on every startup. In production (real DynamoDB)
// tables should be pre-provisioned via IaC; this helper targets local/ExtendDB.
func (c *Client) EnsureTables(ctx context.Context) error {
	if err := c.ensureTable(ctx, c.EventsTable, "pk", "sk"); err != nil {
		return err
	}
	return c.ensureTable(ctx, c.UserSubscriptionsTable, "pk", "sk")
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
