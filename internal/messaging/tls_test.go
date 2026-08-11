// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package messaging_test

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hermes-notifications/hermes/internal/messaging"
)

// ADR 0005 phase 2. These tests are deliberately infrastructure-free: they stand up a
// real TLS listener that speaks just enough of the NATS wire protocol for a client to
// finish connecting, so the assertion is a genuine TLS handshake rather than a mock of
// one. `make test` must not need a NATS server.

// TestConnect_TLSWithCABundle is the property the deployment depends on: given the CA
// that signed the server certificate, a tls:// connection completes.
func TestConnect_TLSWithCABundle(t *testing.T) {
	srv := newTLSTestServer(t)

	client, err := messaging.Connect("tls://localhost:"+srv.port, messaging.WithCABundle(srv.caPath))
	if err != nil {
		t.Fatalf("expected a TLS connection to succeed with the CA bundle, got: %v", err)
	}
	client.Close()
}

// The mirror image, and the one that proves the first test asserts something: without the
// CA the same server is untrusted. nats.go never falls back to plaintext when
// verification fails — it errors — so there is no silent downgrade to worry about.
func TestConnect_TLSWithoutCABundleIsRejected(t *testing.T) {
	srv := newTLSTestServer(t)

	client, err := messaging.Connect("tls://localhost:" + srv.port)
	if err == nil {
		client.Close()
		t.Fatal("expected verification against the system trust store to fail, got a connection")
	}
	if !strings.Contains(err.Error(), "certificate signed by unknown authority") {
		t.Errorf("expected an x509 trust failure, got: %v", err)
	}
}

// A server that requires TLS must reject a plaintext client. nats.go upgrades on its own
// when the server's INFO says tls_required, so the observable failure is the trust error
// — never a plaintext session.
func TestConnect_PlaintextURLAgainstTLSRequiredServerIsRejected(t *testing.T) {
	srv := newTLSTestServer(t)

	client, err := messaging.Connect("nats://localhost:" + srv.port)
	if err == nil {
		client.Close()
		t.Fatal("expected a plaintext URL to fail against a TLS-required server, got a connection")
	}
}

// The development path: `make infra-up` runs NATS with no TLS and sets no CA bundle, so
// an empty path must leave the connection exactly as it was before this option existed.
func TestWithCABundle_EmptyPathLeavesPlaintextWorking(t *testing.T) {
	srv := newPlaintextTestServer(t)

	client, err := messaging.Connect("nats://localhost:"+srv.port, messaging.WithCABundle(""))
	if err != nil {
		t.Fatalf("an empty CA bundle must not change a plaintext connection, got: %v", err)
	}
	client.Close()
}

// A CA bundle that is configured but not mounted is a deployment mistake, and the error
// has to name the path or the operator is guessing.
func TestWithCABundle_UnreadablePathNamesTheFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-mounted", "ca.crt")

	_, err := messaging.Connect("tls://localhost:4222", messaging.WithCABundle(missing))
	if err == nil {
		t.Fatal("expected a missing CA bundle to fail the connection")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error must name the missing bundle %q, got: %v", missing, err)
	}
}

// --- test server -------------------------------------------------------------------

type testServer struct {
	port   string
	caPath string
}

// newTLSTestServer starts a listener that writes a NATS INFO advertising tls_required,
// upgrades the connection to TLS, then answers PING with PONG — the minimum for
// nats.Connect to return. The certificate is signed by a throwaway CA written to disk so
// WithCABundle has a real PEM file to load.
func newTLSTestServer(t *testing.T) testServer {
	t.Helper()
	caPEM, cert := generateCA(t)
	caPath := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(caPath, caPEM, 0o600); err != nil {
		t.Fatalf("write ca: %v", err)
	}
	return testServer{port: serve(t, true, cert), caPath: caPath}
}

func newPlaintextTestServer(t *testing.T) testServer {
	t.Helper()
	return testServer{port: serve(t, false, tls.Certificate{})}
}

func serve(t *testing.T, secure bool, cert tls.Certificate) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleConn(conn, secure, cert)
		}
	}()

	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	return port
}

func handleConn(conn net.Conn, secure bool, cert tls.Certificate) {
	defer func() { _ = conn.Close() }()

	info := `INFO {"server_id":"TESTSERVER","server_name":"test","version":"2.14.3","proto":1,` +
		`"host":"127.0.0.1","port":4222,"headers":true,"max_payload":1048576,"tls_required":false}` + "\r\n"
	if secure {
		info = strings.Replace(info, `"tls_required":false`, `"tls_required":true`, 1)
	}
	if _, err := conn.Write([]byte(info)); err != nil {
		return
	}

	var rw net.Conn = conn
	if secure {
		tlsConn := tls.Server(conn, &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		})
		if err := tlsConn.Handshake(); err != nil {
			return
		}
		rw = tlsConn
	}

	reader := bufio.NewReader(rw)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		if strings.HasPrefix(line, "PING") {
			if _, err := rw.Write([]byte("PONG\r\n")); err != nil {
				return
			}
		}
	}
}

// generateCA returns the CA certificate in PEM form and a leaf certificate for
// "localhost" signed by it.
func generateCA(t *testing.T) ([]byte, tls.Certificate) {
	t.Helper()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ca key: %v", err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "hermes-messaging-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("ca cert: %v", err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatalf("parse ca: %v", err)
	}

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("leaf key: %v", err)
	}
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("leaf cert: %v", err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		tls.Certificate{Certificate: [][]byte{leafDER}, PrivateKey: leafKey}
}
