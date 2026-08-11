// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package auth

import "github.com/hermes-notifications/hermes/internal/models"

// SigningConfigsFromKeys converts a slice of JWTSigningKey models into
// the auth package's JWTSigningConfig representation.
func SigningConfigsFromKeys(keys []models.JWTSigningKey) []JWTSigningConfig {
	configs := make([]JWTSigningConfig, len(keys))
	for i, k := range keys {
		configs[i] = JWTSigningConfig{
			Name:                k.Name,
			Secret:              []byte(k.Secret),
			Algorithm:           k.Algorithm,
			UserIDClaim:         k.UserIDClaim,
			OrganizationIDClaim: k.OrganizationIDClaim,
		}
	}
	return configs
}
