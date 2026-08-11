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

type SubscriptionCategory struct {
	ID              string    `json:"id"`
	Slug            string    `json:"slug"`
	Name            string    `json:"name"`
	DefaultChannels []string  `json:"default_channels"`
	DefaultState    string    `json:"default_state"`
	SortOrder       int       `json:"sort_order"`
	CreatedAt       time.Time `json:"created_at"`
}

type CreateCategoryRequest struct {
	Slug            string   `json:"slug"`
	Name            string   `json:"name"`
	DefaultChannels []string `json:"default_channels,omitempty"`
	DefaultState    string   `json:"default_state"`
	SortOrder       int      `json:"sort_order,omitempty"`
}

type UpdateCategoryRequest struct {
	Name            string   `json:"name"`
	DefaultChannels []string `json:"default_channels"`
	DefaultState    string   `json:"default_state"`
	SortOrder       int      `json:"sort_order"`
}

type CategoriesService struct {
	client *Client
}

func (s *CategoriesService) List(ctx context.Context) ([]SubscriptionCategory, error) {
	req, err := s.client.newRequest(ctx, http.MethodGet, "/v1/subscriptions/categories", nil)
	if err != nil {
		return nil, err
	}
	var categories []SubscriptionCategory
	if err := s.client.do(req, &categories); err != nil {
		return nil, err
	}
	return categories, nil
}

func (s *CategoriesService) Create(ctx context.Context, body CreateCategoryRequest) (*SubscriptionCategory, error) {
	req, err := s.client.newRequest(ctx, http.MethodPost, "/v1/subscriptions/categories", body)
	if err != nil {
		return nil, err
	}
	var cat SubscriptionCategory
	if err := s.client.do(req, &cat); err != nil {
		return nil, err
	}
	return &cat, nil
}

func (s *CategoriesService) Update(ctx context.Context, id string, body UpdateCategoryRequest) (*SubscriptionCategory, error) {
	req, err := s.client.newRequest(ctx, http.MethodPut, fmt.Sprintf("/v1/subscriptions/categories/%s", id), body)
	if err != nil {
		return nil, err
	}
	var cat SubscriptionCategory
	if err := s.client.do(req, &cat); err != nil {
		return nil, err
	}
	return &cat, nil
}

func (s *CategoriesService) Delete(ctx context.Context, id string) error {
	req, err := s.client.newRequest(ctx, http.MethodDelete, fmt.Sprintf("/v1/subscriptions/categories/%s", id), nil)
	if err != nil {
		return err
	}
	return s.client.do(req, nil)
}
