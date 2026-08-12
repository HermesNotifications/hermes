// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package apikeybootstrap

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// KubeSecrets talks to the Kubernetes API with net/http and the pod's own ServiceAccount.
//
// # Why not client-go
//
// client-go is the obvious answer and was rejected on the specifics. The entire interaction is
// one GET and one POST on a single resource type. Against that, client-go pulls a large module
// subtree into a repository that today has exactly one k8s dependency (apimachinery, indirect)
// and thirteen binaries that would all carry it.
//
// The failure modes it would buy are covered here by design rather than by library:
//
//   - Conflicts. A 409 on create is handled explicitly by adopting the winner's Secret, which
//     is a decision about *this* algorithm that no client library could make for us.
//   - Transient errors. The Job has backoffLimit 6, so Kubernetes is the retry loop. A second
//     retry loop inside the process would only make the failure slower to surface.
//   - Token rotation. Projected ServiceAccount tokens are refreshed on disk by the kubelet, so
//     the token is read fresh on every request rather than cached at construction.
//
// cmd/natskeys sets the precedent in the same direction: it generates credentials outside the
// cluster and prints `kubectl create secret` rather than reaching for an API client.
type KubeSecrets struct {
	server    string
	namespace string
	tokenPath string
	client    *http.Client
}

// The standard in-pod ServiceAccount mount.
const saDir = "/var/run/secrets/kubernetes.io/serviceaccount"

// NewKubeSecrets builds a client from the ambient pod environment.
func NewKubeSecrets() (*KubeSecrets, error) {
	return newKubeSecrets(saDir, os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT"))
}

func newKubeSecrets(dir, host, port string) (*KubeSecrets, error) {
	if host == "" || port == "" {
		return nil, fmt.Errorf("KUBERNETES_SERVICE_HOST/PORT are unset; this must run as a pod")
	}

	namespace, err := os.ReadFile(filepath.Join(dir, "namespace"))
	if err != nil {
		return nil, fmt.Errorf("read namespace: %w", err)
	}
	caPEM, err := os.ReadFile(filepath.Join(dir, "ca.crt"))
	if err != nil {
		return nil, fmt.Errorf("read cluster CA: %w", err)
	}

	// The API server's CA, not the system roots. The image is FROM scratch and carries
	// ca-certificates for outbound TLS; the in-cluster API server is signed by the cluster's
	// own CA and would not verify against those.
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("cluster CA at %s/ca.crt contains no usable certificate", dir)
	}

	return &KubeSecrets{
		server:    fmt.Sprintf("https://%s:%s", host, port),
		namespace: string(bytes.TrimSpace(namespace)),
		tokenPath: filepath.Join(dir, "token"),
		client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}},
		},
	}, nil
}

// Namespace is the namespace this client operates in.
func (k *KubeSecrets) Namespace() string { return k.namespace }

func (k *KubeSecrets) do(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
	// Read the token per request: projected tokens are rotated on disk by the kubelet, and a
	// token cached at construction expires under a Job that is retrying.
	token, err := os.ReadFile(k.tokenPath)
	if err != nil {
		return 0, nil, fmt.Errorf("read service account token: %w", err)
	}

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, k.server+path, reader)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+string(bytes.TrimSpace(token)))
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := k.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, payload, nil
}

type secretPayload struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   secretMeta        `json:"metadata"`
	Type       string            `json:"type,omitempty"`
	Data       map[string]string `json:"data"`
}

type secretMeta struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// GetSecret implements SecretStore.
func (k *KubeSecrets) GetSecret(ctx context.Context, name string) (map[string][]byte, bool, error) {
	status, body, err := k.do(ctx, http.MethodGet, k.path(name), nil)
	if err != nil {
		return nil, false, err
	}
	switch status {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, false, nil
	case http.StatusForbidden, http.StatusUnauthorized:
		return nil, false, fmt.Errorf(
			"not allowed to read secret %s/%s (%d): the bootstrap ServiceAccount needs get on this secret. %s",
			k.namespace, name, status, string(body))
	default:
		return nil, false, fmt.Errorf("get secret %s/%s: %d %s", k.namespace, name, status, string(body))
	}

	var payload secretPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false, fmt.Errorf("decode secret %s: %w", name, err)
	}
	// The API returns data base64-encoded on the wire, on top of the JSON encoding.
	data := make(map[string][]byte, len(payload.Data))
	for key, encoded := range payload.Data {
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, false, fmt.Errorf("decode secret %s key %s: %w", name, key, err)
		}
		data[key] = decoded
	}
	return data, true, nil
}

// CreateSecret implements SecretStore. Returns ErrAlreadyExists on 409.
func (k *KubeSecrets) CreateSecret(ctx context.Context, name string, data map[string][]byte) error {
	encoded := make(map[string]string, len(data))
	for key, value := range data {
		encoded[key] = base64.StdEncoding.EncodeToString(value)
	}
	body, err := json.Marshal(secretPayload{
		APIVersion: "v1",
		Kind:       "Secret",
		Metadata: secretMeta{
			Name:      name,
			Namespace: k.namespace,
			// Not helm.sh/* labels and no ownerReference on purpose. Helm does not track this
			// Secret, so `helm uninstall` leaves it -- which is what lets a reinstall against a
			// surviving database re-adopt the key the operator already has.
			Labels: map[string]string{"app.kubernetes.io/part-of": "hermes"},
		},
		Type: "Opaque",
		Data: encoded,
	})
	if err != nil {
		return err
	}

	status, respBody, err := k.do(ctx, http.MethodPost,
		fmt.Sprintf("/api/v1/namespaces/%s/secrets", k.namespace), body)
	if err != nil {
		return err
	}
	switch status {
	case http.StatusOK, http.StatusCreated:
		return nil
	case http.StatusConflict:
		return ErrAlreadyExists
	case http.StatusForbidden, http.StatusUnauthorized:
		return fmt.Errorf(
			"not allowed to create secret %s/%s (%d): the bootstrap ServiceAccount needs create on secrets. %s",
			k.namespace, name, status, string(respBody))
	default:
		return fmt.Errorf("create secret %s/%s: %d %s", k.namespace, name, status, string(respBody))
	}
}

func (k *KubeSecrets) path(name string) string {
	return fmt.Sprintf("/api/v1/namespaces/%s/secrets/%s", k.namespace, name)
}
