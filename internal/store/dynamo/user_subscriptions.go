// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package dynamo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/jackc/pgx/v5"

	"github.com/hermesnotifications/hermes/internal/models"
)

// UserSubscriptionStore implements store.UserSubscriptionRepository using
// DynamoDB. This is a pure KV table — no delegation to Postgres required.
//
// Table: hermes-user-subscriptions
//   pk:  USER#<user_id>
//   sk:  SUB#<subscription_id>
type UserSubscriptionStore struct {
	client *Client
}

// NewUserSubscriptionStore creates a UserSubscriptionStore.
func NewUserSubscriptionStore(client *Client) *UserSubscriptionStore {
	return &UserSubscriptionStore{client: client}
}

// GetUserSubscription fetches a single user subscription by (userID, subscriptionID).
func (s *UserSubscriptionStore) GetUserSubscription(ctx context.Context, userID, subscriptionID string) (*models.UserSubscription, error) {
	out, err := s.client.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.client.UserSubscriptionsTable),
		Key: map[string]types.AttributeValue{
			"pk": strVal("USER#" + userID),
			"sk": strVal("SUB#" + subscriptionID),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("dynamo get user subscription: %w", err)
	}
	if out.Item == nil {
		return nil, fmt.Errorf("get user subscription: %w", pgx.ErrNoRows)
	}
	return unmarshalUserSubscription(out.Item)
}

// GetUserSubscriptions returns all subscriptions for a user.
func (s *UserSubscriptionStore) GetUserSubscriptions(ctx context.Context, userID string) ([]models.UserSubscription, error) {
	out, err := s.client.db.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.client.UserSubscriptionsTable),
		KeyConditionExpression: aws.String("pk = :pk AND begins_with(sk, :prefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":     strVal("USER#" + userID),
			":prefix": strVal("SUB#"),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("dynamo get user subscriptions: %w", err)
	}

	subs := make([]models.UserSubscription, 0, len(out.Items))
	for _, item := range out.Items {
		us, err := unmarshalUserSubscription(item)
		if err != nil {
			return nil, err
		}
		subs = append(subs, *us)
	}
	return subs, nil
}

// SetUserSubscription upserts a user subscription (creates or overwrites).
func (s *UserSubscriptionStore) SetUserSubscription(ctx context.Context, userID, subscriptionID string, optedIn bool) (*models.UserSubscription, error) {
	now := time.Now().UTC()
	us := &models.UserSubscription{
		UserID:         userID,
		SubscriptionID: subscriptionID,
		OptedIn:        optedIn,
		CreatedAt:      now,
	}

	_, err := s.client.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.client.UserSubscriptionsTable),
		Item: map[string]types.AttributeValue{
			"pk":              strVal("USER#" + userID),
			"sk":              strVal("SUB#" + subscriptionID),
			"user_id":         strVal(userID),
			"subscription_id": strVal(subscriptionID),
			"opted_in":        boolVal(optedIn),
			"created_at":      strVal(now.Format(time.RFC3339Nano)),
		},
		// Preserve created_at if item already exists.
		ConditionExpression: aws.String("attribute_not_exists(pk)"),
	})
	if err != nil {
		// If the item already exists the condition fails — retry without the
		// condition to perform the upsert, preserving the original created_at.
		var ccfe *types.ConditionalCheckFailedException
		if errors.As(err, &ccfe) {
			return s.updateUserSubscription(ctx, userID, subscriptionID, optedIn)
		}
		return nil, fmt.Errorf("dynamo set user subscription: %w", err)
	}
	return us, nil
}

// updateUserSubscription updates opted_in on an existing item, leaving
// created_at unchanged.
func (s *UserSubscriptionStore) updateUserSubscription(ctx context.Context, userID, subscriptionID string, optedIn bool) (*models.UserSubscription, error) {
	out, err := s.client.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.client.UserSubscriptionsTable),
		Key: map[string]types.AttributeValue{
			"pk": strVal("USER#" + userID),
			"sk": strVal("SUB#" + subscriptionID),
		},
		UpdateExpression: aws.String("SET opted_in = :v"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":v": boolVal(optedIn),
		},
		ReturnValues: types.ReturnValueAllNew,
	})
	if err != nil {
		return nil, fmt.Errorf("dynamo update user subscription: %w", err)
	}
	return unmarshalUserSubscription(out.Attributes)
}

// DeleteUserSubscription removes a user subscription.
func (s *UserSubscriptionStore) DeleteUserSubscription(ctx context.Context, userID, subscriptionID string) error {
	out, err := s.client.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(s.client.UserSubscriptionsTable),
		Key: map[string]types.AttributeValue{
			"pk": strVal("USER#" + userID),
			"sk": strVal("SUB#" + subscriptionID),
		},
		ConditionExpression: aws.String("attribute_exists(pk)"),
		ReturnValues:        types.ReturnValueAllOld,
	})
	if err != nil {
		var ccfe *types.ConditionalCheckFailedException
		if errors.As(err, &ccfe) {
			return fmt.Errorf("delete user subscription: %w", pgx.ErrNoRows)
		}
		return fmt.Errorf("dynamo delete user subscription: %w", err)
	}
	_ = out
	return nil
}

// unmarshalUserSubscription converts a raw DynamoDB item into a UserSubscription.
func unmarshalUserSubscription(item map[string]types.AttributeValue) (*models.UserSubscription, error) {
	us := &models.UserSubscription{}

	if v, ok := item["user_id"].(*types.AttributeValueMemberS); ok {
		us.UserID = v.Value
	}
	if v, ok := item["subscription_id"].(*types.AttributeValueMemberS); ok {
		us.SubscriptionID = v.Value
	}
	if v, ok := item["opted_in"].(*types.AttributeValueMemberBOOL); ok {
		us.OptedIn = v.Value
	}
	if v, ok := item["created_at"].(*types.AttributeValueMemberS); ok {
		t, err := time.Parse(time.RFC3339Nano, v.Value)
		if err != nil {
			return nil, fmt.Errorf("parse created_at: %w", err)
		}
		us.CreatedAt = t
	}
	return us, nil
}

