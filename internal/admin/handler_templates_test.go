// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package admin_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hermesnotifications/hermes/internal/auth"
)

// Finding 3. Every template operation is gated on templates:manage, not just the read.
// A gate on the list endpoint alone would be worse than none: it reads as protected while
// leaving create, update and delete open.
//
// handler_types_test.go predates the types → templates rename and covers the handlers'
// behaviour; this file covers their authorization.
func TestTemplateHandlers_AllOperationsRequireTemplatesManage(t *testing.T) {
	// Each body must satisfy its own schema. Huma validates input BEFORE invoking the
	// handler, so a malformed request from an unauthorized caller returns 422 and never
	// reaches the permission check — using one shared body here would have made these
	// cases pass or fail for reasons unrelated to authorization.
	ops := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"list is gated", http.MethodGet, "/v1/templates", ""},
		{"create is gated", http.MethodPost, "/v1/templates", `{"slug":"welcome","name":"Welcome","default_channels":["email"]}`},
		{"update is gated", http.MethodPut, "/v1/templates/ntpl-1", `{"name":"Welcome"}`},
		{"delete is gated", http.MethodDelete, "/v1/templates/ntpl-1", ""},
	}

	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			// A key with a real permission, just not this one — so a failure means the
			// route is ungated rather than that authentication is broken.
			handler, key := authenticatedAdmin(t, auth.PermNotificationsSend)

			var reader *strings.Reader
			if op.body != "" {
				reader = strings.NewReader(op.body)
			} else {
				reader = strings.NewReader("")
			}
			req := httptest.NewRequest(op.method, op.path, reader)
			req.Header.Set("Authorization", "Bearer "+key)
			if op.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Errorf("%s %s: expected 403 for a key without %s, got %d",
					op.method, op.path, auth.PermTemplatesManage, rec.Code)
			}
		})
	}
}

func TestTemplateHandlers_AllowAKeyHoldingTemplatesManage(t *testing.T) {
	handler, key := authenticatedAdmin(t, auth.PermTemplatesManage)

	req := httptest.NewRequest(http.MethodGet, "/v1/templates", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code == http.StatusForbidden {
		t.Fatalf("a key holding %s was refused", auth.PermTemplatesManage)
	}
}
