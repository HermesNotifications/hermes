// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package apikeybootstrap

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// kubeFixture stands up a fake API server and a ServiceAccount directory pointing at it, so the
// client is exercised over real HTTP rather than through an interface it also defines.
func kubeFixture(t *testing.T, handler http.HandlerFunc) (*KubeSecrets, *httptest.Server) {
	t.Helper()

	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)

	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("namespace", "hermes\n")
	write("token", "fake-token\n")

	// The fixture server's own certificate, so the client verifies against it exactly as it
	// would against a real cluster CA.
	certPEM := pemEncode(server.Certificate().Raw)
	write("ca.crt", certPEM)

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse fixture url: %v", err)
	}
	client, err := newKubeSecrets(dir, parsed.Hostname(), parsed.Port())
	if err != nil {
		t.Fatalf("newKubeSecrets: %v", err)
	}
	return client, server
}

func pemEncode(der []byte) string {
	const width = 64
	encoded := base64.StdEncoding.EncodeToString(der)
	var sb strings.Builder
	sb.WriteString("-----BEGIN CERTIFICATE-----\n")
	for len(encoded) > width {
		sb.WriteString(encoded[:width] + "\n")
		encoded = encoded[width:]
	}
	sb.WriteString(encoded + "\n-----END CERTIFICATE-----\n")
	return sb.String()
}

func TestGetSecretDecodesBase64Data(t *testing.T) {
	client, _ := kubeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/namespaces/hermes/secrets/hermes-bootstrap" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fake-token" {
			t.Errorf("Authorization = %q; the trailing newline in the token file must be trimmed", got)
		}
		_ = json.NewEncoder(w).Encode(secretPayload{
			Data: map[string]string{SecretKey: base64.StdEncoding.EncodeToString([]byte("hms_abc"))},
		})
	})

	data, found, err := client.GetSecret(context.Background(), "hermes-bootstrap")
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if !found {
		t.Fatal("found = false")
	}
	if got := string(data[SecretKey]); got != "hms_abc" {
		t.Errorf("data = %q, want hms_abc", got)
	}
}

func TestGetSecretTreats404AsAbsentRatherThanAnError(t *testing.T) {
	client, _ := kubeFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	_, found, err := client.GetSecret(context.Background(), "hermes-bootstrap")
	if err != nil {
		t.Fatalf("a missing Secret is the fresh-install path, not an error: %v", err)
	}
	if found {
		t.Error("found = true")
	}
}

func TestGetSecretExplainsA403(t *testing.T) {
	// The likeliest real misconfiguration. A bare "403" sends someone to the API server logs;
	// naming the missing verb sends them to the RBAC.
	client, _ := kubeFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})

	_, _, err := client.GetSecret(context.Background(), "hermes-bootstrap")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "ServiceAccount") {
		t.Errorf("error does not mention the ServiceAccount: %v", err)
	}
}

func TestCreateSecretPostsBase64EncodedData(t *testing.T) {
	var got secretPayload
	client, _ := kubeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if r.URL.Path != "/api/v1/namespaces/hermes/secrets" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusCreated)
	})

	err := client.CreateSecret(context.Background(), "hermes-bootstrap",
		map[string][]byte{SecretKey: []byte("hms_xyz")})
	if err != nil {
		t.Fatalf("CreateSecret: %v", err)
	}

	if got.Kind != "Secret" || got.APIVersion != "v1" {
		t.Errorf("payload = %s/%s", got.APIVersion, got.Kind)
	}
	if got.Metadata.Namespace != "hermes" {
		t.Errorf("namespace = %q", got.Metadata.Namespace)
	}
	decoded, err := base64.StdEncoding.DecodeString(got.Data[SecretKey])
	if err != nil {
		t.Fatalf("data was not base64: %v", err)
	}
	if string(decoded) != "hms_xyz" {
		t.Errorf("decoded = %q", decoded)
	}
}

func TestCreateSecretMapsConflictToErrAlreadyExists(t *testing.T) {
	// Run() distinguishes "someone else won the race" from a real failure by this error alone.
	client, _ := kubeFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
	})

	err := client.CreateSecret(context.Background(), "hermes-bootstrap", map[string][]byte{SecretKey: []byte("x")})
	if err != ErrAlreadyExists {
		t.Fatalf("err = %v, want ErrAlreadyExists", err)
	}
}

func TestRefusesToRunOutsideAPod(t *testing.T) {
	if _, err := newKubeSecrets(t.TempDir(), "", ""); err == nil {
		t.Fatal("expected an error when KUBERNETES_SERVICE_HOST is unset")
	}
}

func TestRefusesAnUnusableClusterCA(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"namespace": "hermes", "token": "t", "ca.crt": "not a certificate",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// Falling back to the system pool here would mean trusting the public web to vouch for the
	// API server, so an unreadable CA must fail rather than degrade.
	if _, err := newKubeSecrets(dir, "10.0.0.1", "443"); err == nil {
		t.Fatal("expected an error for an unparseable CA")
	}
}
