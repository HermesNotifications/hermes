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

// TenantRepository wraps a store.TenantRepository with Redis caching.
// Cache errors are non-fatal: on failure, requests fall through to the backing store.
type TenantRepository struct {
	store store.TenantRepository
	cache *cache.Client
}

func NewTenantRepository(store store.TenantRepository, cache *cache.Client) *TenantRepository {
	return &TenantRepository{store: store, cache: cache}
}

func (r *TenantRepository) EnsureTenant(ctx context.Context, id string) (*models.Tenant, error) {
	if r.cache != nil {
		if exists, err := r.cache.TenantExists(ctx, id); err == nil && exists {
			return &models.Tenant{ID: id}, nil
		}
	}

	t, err := r.store.EnsureTenant(ctx, id)
	if err != nil {
		return nil, err
	}

	if r.cache != nil {
		_ = r.cache.SetTenantExists(ctx, id, 24*time.Hour)
	}

	return t, nil
}

func (r *TenantRepository) CreateTenant(ctx context.Context, id, name string) (*models.Tenant, error) {
	return r.store.CreateTenant(ctx, id, name)
}

func (r *TenantRepository) GetTenantByID(ctx context.Context, id string) (*models.Tenant, error) {
	return r.store.GetTenantByID(ctx, id)
}

func (r *TenantRepository) ListTenants(ctx context.Context) ([]models.Tenant, error) {
	return r.store.ListTenants(ctx)
}

func (r *TenantRepository) CountUsersByTenant(ctx context.Context) (map[string]int, error) {
	return r.store.CountUsersByTenant(ctx)
}
