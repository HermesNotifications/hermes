// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package client

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type NotificationTemplate struct {
	ID              string                       `json:"id"`
	SubscriptionID  *string                      `json:"subscription_id,omitempty"`
	Slug            string                       `json:"slug"`
	Name            string                       `json:"name"`
	DefaultChannels []string                     `json:"default_channels"`
	Content         map[string]map[string]string `json:"content,omitempty"`
	CreatedAt       time.Time                    `json:"created_at"`
}

type CreateTemplateRequest struct {
	Slug            string                       `json:"slug"`
	Name            string                       `json:"name"`
	SubscriptionID  *string                      `json:"subscription_id,omitempty"`
	DefaultChannels []string                     `json:"default_channels,omitempty"`
	Content         map[string]map[string]string `json:"content,omitempty"`
}

type UpdateTemplateRequest struct {
	Name            string                       `json:"name"`
	SubscriptionID  *string                      `json:"subscription_id,omitempty"`
	DefaultChannels []string                     `json:"default_channels,omitempty"`
	Content         map[string]map[string]string `json:"content,omitempty"`
}

type TemplatesService struct{ client *Client }

func (s *TemplatesService) List(ctx context.Context) ([]NotificationTemplate, error) {
	req, err := s.client.newRequest(ctx, http.MethodGet, "/v1/templates", nil)
	if err != nil {
		return nil, err
	}
	var templates []NotificationTemplate
	if err := s.client.do(req, &templates); err != nil {
		return nil, err
	}
	return templates, nil
}

func (s *TemplatesService) Create(ctx context.Context, body CreateTemplateRequest) (*NotificationTemplate, error) {
	req, err := s.client.newRequest(ctx, http.MethodPost, "/v1/templates", body)
	if err != nil {
		return nil, err
	}
	var nt NotificationTemplate
	if err := s.client.do(req, &nt); err != nil {
		return nil, err
	}
	return &nt, nil
}

func (s *TemplatesService) Update(ctx context.Context, id string, body UpdateTemplateRequest) (*NotificationTemplate, error) {
	req, err := s.client.newRequest(ctx, http.MethodPut, fmt.Sprintf("/v1/templates/%s", id), body)
	if err != nil {
		return nil, err
	}
	var nt NotificationTemplate
	if err := s.client.do(req, &nt); err != nil {
		return nil, err
	}
	return &nt, nil
}

func (s *TemplatesService) Delete(ctx context.Context, id string) error {
	req, err := s.client.newRequest(ctx, http.MethodDelete, fmt.Sprintf("/v1/templates/%s", id), nil)
	if err != nil {
		return err
	}
	return s.client.do(req, nil)
}
