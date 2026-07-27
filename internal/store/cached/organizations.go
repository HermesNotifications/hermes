// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package cached

import (
	"context"
	"time"

	"github.com/hermes-notifications/hermes/internal/cache"
	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/hermes-notifications/hermes/internal/store"
)

// OrganizationRepository wraps a store.OrganizationRepository with Redis caching.
// Cache errors are non-fatal: on failure, requests fall through to the backing store.
type OrganizationRepository struct {
	store store.OrganizationRepository
	cache *cache.Client
}

func NewOrganizationRepository(store store.OrganizationRepository, cache *cache.Client) *OrganizationRepository {
	return &OrganizationRepository{store: store, cache: cache}
}

func (r *OrganizationRepository) EnsureOrganization(ctx context.Context, id string) (*models.Organization, error) {
	if r.cache != nil {
		if exists, err := r.cache.OrganizationExists(ctx, id); err == nil && exists {
			return &models.Organization{ID: id}, nil
		}
	}

	t, err := r.store.EnsureOrganization(ctx, id)
	if err != nil {
		return nil, err
	}

	if r.cache != nil {
		_ = r.cache.SetOrganizationExists(ctx, id, 24*time.Hour)
	}

	return t, nil
}

func (r *OrganizationRepository) CreateOrganization(ctx context.Context, id, name string) (*models.Organization, error) {
	return r.store.CreateOrganization(ctx, id, name)
}

func (r *OrganizationRepository) GetOrganizationByID(ctx context.Context, id string) (*models.Organization, error) {
	return r.store.GetOrganizationByID(ctx, id)
}

func (r *OrganizationRepository) ListOrganizations(ctx context.Context) ([]models.Organization, error) {
	return r.store.ListOrganizations(ctx)
}

func (r *OrganizationRepository) CountUsersByOrganization(ctx context.Context) (map[string]int, error) {
	return r.store.CountUsersByOrganization(ctx)
}
