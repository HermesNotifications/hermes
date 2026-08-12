// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package auth

import (
	"context"
	"errors"
	"fmt"
)

const (
	PermAPIKeysManage       = "apikeys:manage"
	PermNotificationsSend   = "notifications:send"
	PermTemplatesManage     = "templates:manage"
	PermOrganizationsManage = "organizations:manage"
)

var AllPermissions = []string{
	PermAPIKeysManage,
	PermNotificationsSend,
	PermTemplatesManage,
	PermOrganizationsManage,
}

var DefaultPermissions = []string{
	PermNotificationsSend,
	PermTemplatesManage,
	PermOrganizationsManage,
}

type ValidatedKey struct {
	ID          string
	Permissions []string

	// RateLimitPerSecond and RateLimitBurst carry this credential's own limits
	// so the rate limiter, which runs immediately after authentication, can read
	// them without a second lookup. Zero means "use the service default", which
	// is the sentinel middleware.ResolveLimit already understands.
	RateLimitPerSecond int
	RateLimitBurst     int
}

const validatedKeyContextKey contextKey = "validatedKey"

func WithValidatedKey(ctx context.Context, key *ValidatedKey) context.Context {
	return context.WithValue(ctx, validatedKeyContextKey, key)
}

func GetValidatedKey(ctx context.Context) *ValidatedKey {
	v, _ := ctx.Value(validatedKeyContextKey).(*ValidatedKey)
	return v
}

var (
	// ErrNoAPIKey means no validated key reached the handler. Callers map it to 401.
	ErrNoAPIKey = errors.New("no validated API key in context")
	// ErrInsufficientPermission means the key is valid but lacks the permission. 403.
	ErrInsufficientPermission = errors.New("insufficient permissions")
)

// CheckPermission reports whether the API key in ctx may perform perm.
//
// Finding 3. This replaces a RequirePermission that returned
// func(http.Handler) http.Handler. That shape is why it had zero production call sites
// while being fully unit-tested: services route through Huma, whose handlers are
// func(ctx, input) and never see an http.Handler, so the middleware could not be applied
// to any operation. A context-taking function is what a Huma handler can actually call.
//
// It fails CLOSED. The three inline checks this consolidates were written
// `if key != nil && !HasPermission(...)`, which passes when the key is nil — a route
// reached without a validated key was granted every permission. That was unreachable in
// production because APIKeyMiddleware 401s first, but it is a latent hazard that becomes
// live the moment a route is mounted differently. Nothing here depends on a nil key being
// impossible; see SkipAuthMiddleware for how tests get a key instead of an exemption.
func CheckPermission(ctx context.Context, perm string) error {
	key := GetValidatedKey(ctx)
	if key == nil {
		return ErrNoAPIKey
	}
	if !HasPermission(key, perm) {
		return fmt.Errorf("%w: %s", ErrInsufficientPermission, perm)
	}
	return nil
}

func ValidatePermissions(perms []string) error {
	valid := make(map[string]bool, len(AllPermissions))
	for _, p := range AllPermissions {
		valid[p] = true
	}
	for _, p := range perms {
		if !valid[p] {
			return fmt.Errorf("unknown permission: %s", p)
		}
	}
	return nil
}

func HasPermission(key *ValidatedKey, perm string) bool {
	for _, p := range key.Permissions {
		if p == perm {
			return true
		}
	}
	return false
}
