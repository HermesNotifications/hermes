package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hermes-notifications/hermes/internal/auth"
)

func TestRequirePermission_Allowed(t *testing.T) {
	handler := auth.RequirePermission(auth.PermNotificationsSend)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(auth.WithValidatedKey(req.Context(), &auth.ValidatedKey{
		ID:          "key_abc123",
		Permissions: []string{auth.PermNotificationsSend, auth.PermTemplatesManage},
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestRequirePermission_Denied(t *testing.T) {
	handler := auth.RequirePermission(auth.PermAPIKeysManage)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(auth.WithValidatedKey(req.Context(), &auth.ValidatedKey{
		ID:          "key_abc123",
		Permissions: []string{auth.PermNotificationsSend},
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestRequirePermission_NoKeyInContext(t *testing.T) {
	handler := auth.RequirePermission(auth.PermNotificationsSend)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestValidatePermissions(t *testing.T) {
	valid := []string{auth.PermNotificationsSend, auth.PermTemplatesManage}
	if err := auth.ValidatePermissions(valid); err != nil {
		t.Fatalf("expected valid: %v", err)
	}

	invalid := []string{auth.PermNotificationsSend, "foo:bar"}
	if err := auth.ValidatePermissions(invalid); err == nil {
		t.Fatal("expected error for unknown permission")
	}
}
