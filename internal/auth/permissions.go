// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package auth

import (
	"context"
	"fmt"
	"net/http"
)

const (
	PermAPIKeysManage     = "apikeys:manage"
	PermNotificationsSend = "notifications:send"
	PermTemplatesManage   = "templates:manage"
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
}

const validatedKeyContextKey contextKey = "validatedKey"

func WithValidatedKey(ctx context.Context, key *ValidatedKey) context.Context {
	return context.WithValue(ctx, validatedKeyContextKey, key)
}

func GetValidatedKey(ctx context.Context) *ValidatedKey {
	v, _ := ctx.Value(validatedKeyContextKey).(*ValidatedKey)
	return v
}

func RequirePermission(perm string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := GetValidatedKey(r.Context())
			if key == nil {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			for _, p := range key.Permissions {
				if p == perm {
					next.ServeHTTP(w, r)
					return
				}
			}
			http.Error(w, `{"error":"insufficient permissions"}`, http.StatusForbidden)
		})
	}
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
