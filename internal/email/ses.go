// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package email

import (
	"context"
	"fmt"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-sdk-go-v2/otelaws"
)

// SESProvider sends emails via AWS SES v2.
type SESProvider struct {
	client *sesv2.Client
}

// NewSESProvider creates a new SES email provider.
func NewSESProvider(cfg Config) (*SESProvider, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(cfg.SESRegion),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	otelaws.AppendMiddlewares(&awsCfg.APIOptions)

	return &SESProvider{
		client: sesv2.NewFromConfig(awsCfg),
	}, nil
}

func (s *SESProvider) Name() string { return "ses" }

func (s *SESProvider) Send(ctx context.Context, e Email) (string, error) {
	body := &types.Body{}
	if e.HTMLBody != "" {
		body.Html = &types.Content{
			Data: &e.HTMLBody,
		}
	}
	if e.TextBody != "" {
		body.Text = &types.Content{
			Data: &e.TextBody,
		}
	}

	input := &sesv2.SendEmailInput{
		FromEmailAddress: &e.From,
		Destination: &types.Destination{
			ToAddresses: []string{e.To},
		},
		Content: &types.EmailContent{
			Simple: &types.Message{
				Subject: &types.Content{
					Data: &e.Subject,
				},
				Body: body,
			},
		},
	}

	if e.ReplyTo != "" {
		input.ReplyToAddresses = []string{e.ReplyTo}
	}

	result, err := s.client.SendEmail(ctx, input)
	if err != nil {
		return "", fmt.Errorf("ses send: %w", err)
	}

	providerID := ""
	if result.MessageId != nil {
		providerID = *result.MessageId
	}
	return providerID, nil
}
