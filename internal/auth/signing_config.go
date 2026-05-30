// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package auth

import "github.com/hermes-notifications/hermes/internal/models"

// SigningConfigsFromKeys converts a slice of JWTSigningKey models into
// the auth package's JWTSigningConfig representation.
func SigningConfigsFromKeys(keys []models.JWTSigningKey) []JWTSigningConfig {
	configs := make([]JWTSigningConfig, len(keys))
	for i, k := range keys {
		configs[i] = JWTSigningConfig{
			Name:          k.Name,
			Secret:        []byte(k.Secret),
			Algorithm:     k.Algorithm,
			UserIDClaim:   k.UserIDClaim,
			TenantIDClaim: k.TenantIDClaim,
		}
	}
	return configs
}
