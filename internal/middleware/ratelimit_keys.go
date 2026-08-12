// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package middleware

import (
	"net/http"

	"github.com/hermesnotifications/hermes/internal/auth"
)

// APIKeyLimitKey buckets by validated key ID rather than by the raw
// Authorization header.
//
// The header is the wrong key twice over. APIKeyMiddleware strips the "Bearer "
// prefix before validating, so "hms_x" and "Bearer hms_x" are one credential but
// were two buckets — a free doubling of anyone's limit. And the raw header is
// the secret itself, so it had to be hashed before it could be retained. A key
// ID is neither ambiguous nor sensitive.
//
// Send and Admin each held their own copy of this; one copy means one place for
// the next person to reason about the bucketing rule.
func APIKeyLimitKey(r *http.Request) string {
	if k := auth.GetValidatedKey(r.Context()); k != nil {
		return k.ID
	}
	return ""
}

// APIKeyLimits reads the per-credential limit set on the key itself.
//
// Returning (0, 0) — an unset limit, or no validated key — means the service
// default applies, which is the sentinel ResolveLimit already uses.
func APIKeyLimits(r *http.Request) (burst, perSecond int) {
	if k := auth.GetValidatedKey(r.Context()); k != nil {
		return k.RateLimitBurst, k.RateLimitPerSecond
	}
	return 0, 0
}

// UserLimitKey buckets the JWT-authenticated APIs by end user.
func UserLimitKey(r *http.Request) string {
	return auth.UserIDFromContext(r.Context())
}
