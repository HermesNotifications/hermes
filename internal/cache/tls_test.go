// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package cache

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeCA writes a self-signed CA certificate to a PEM file and returns its path.
func writeCA(t *testing.T) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	path := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write ca: %v", err)
	}
	return path
}

func TestCAPoolReadsAPEMBundle(t *testing.T) {
	pool, err := caPool(writeCA(t))
	if err != nil {
		t.Fatalf("caPool: %v", err)
	}
	if pool == nil {
		t.Fatal("pool is nil")
	}
}

func TestCAPoolRejectsAFileThatIsNotACertificate(t *testing.T) {
	// AppendCertsFromPEM signals failure only by returning false. Without an explicit check
	// the result is an empty pool that rejects every certificate with a generic verification
	// error, a long way from the mistyped path that caused it.
	path := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(path, []byte("this is not a certificate"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := caPool(path); err == nil {
		t.Fatal("expected an error for a file containing no certificate")
	}
}

func TestCAPoolRejectsAMissingFile(t *testing.T) {
	if _, err := caPool(filepath.Join(t.TempDir(), "absent.crt")); err == nil {
		t.Fatal("expected an error for a missing bundle")
	}
}

func TestCABundleWithoutTLSIsRefused(t *testing.T) {
	// A CA bundle against a redis:// URL means the operator believes the connection is
	// verified when it would not even be encrypted. Enabling TLS on their behalf would be a
	// worse answer than refusing: it silently changes what the server must support.
	_, err := ConnectWithOptions("redis://localhost:6379/0", Options{CABundle: writeCA(t)})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "rediss://") {
		t.Errorf("error should name the scheme mismatch, got: %v", err)
	}
}

func TestUnreadableBundleFailsRatherThanFallingBackToTheSystemPool(t *testing.T) {
	// The dangerous failure mode: a typo'd path quietly meaning "verify against the public
	// web", so a publicly-trusted certificate for the hostname would be accepted.
	_, err := ConnectWithOptions("rediss://localhost:6379/0",
		Options{CABundle: filepath.Join(t.TempDir(), "typo.crt")})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "CA bundle") {
		t.Errorf("error should name the bundle, got: %v", err)
	}
}

func TestNoBundleLeavesTheURLBehaviourAlone(t *testing.T) {
	// The local-development path: plaintext Redis, no CA, and this must stay a no-op. The
	// connection still fails here because nothing is listening, but it must fail on dialling
	// rather than on configuration.
	_, err := ConnectWithOptions("redis://127.0.0.1:1/0", Options{})
	if err == nil {
		t.Skip("something is listening on port 1; cannot assert the failure mode")
	}
	if strings.Contains(err.Error(), "CA bundle") {
		t.Errorf("empty CABundle was treated as configured: %v", err)
	}
}
