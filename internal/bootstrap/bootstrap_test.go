// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package bootstrap_test

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hermes-notifications/hermes/internal/bootstrap"
	"github.com/hermes-notifications/hermes/internal/messaging"
)

// ADR 0005 phase 2. MustConnectNATS is the only place the services reach the bus, so it
// is the place that has to carry the TLS option through from configuration.
//
// Note the failure shape: MustConnectNATS calls os.Exit(1) rather than returning an
// error, which is the deliberate fail-closed behaviour. So if the option were dropped on
// the floor here, this test does not report a mismatch — the test binary exits 1 and
// `go test` reports the package as failed. Either way it is red, which is what matters.
func TestMustConnectNATS_ForwardsTLSOptions(t *testing.T) {
	srv := newTLSNATS(t)

	client := bootstrap.MustConnectNATS(
		"tls://localhost:"+srv.port,
		quietLogger(),
		messaging.WithCABundle(srv.caPath),
	)
	if client == nil {
		t.Fatal("expected a client")
	}
	client.Close()
}

// The development path: no certificates anywhere, an empty CA bundle, plaintext NATS.
// `make infra-up` must keep working exactly as it did before phase 2.
func TestMustConnectNATS_PlaintextDevelopmentPath(t *testing.T) {
	srv := newPlaintextNATS(t)

	client := bootstrap.MustConnectNATS(
		"nats://localhost:"+srv.port,
		quietLogger(),
		messaging.WithCABundle(""),
	)
	if client == nil {
		t.Fatal("expected a client")
	}
	client.Close()
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --- minimal NATS server ------------------------------------------------------------

type fakeNATS struct {
	port   string
	caPath string
}

// newTLSNATS writes a NATS INFO advertising tls_required, upgrades to TLS with a
// certificate signed by a throwaway CA, then answers PING with PONG — the minimum for
// nats.Connect to return. The CA is written to disk so WithCABundle has a real file.
func newTLSNATS(t *testing.T) fakeNATS {
	t.Helper()
	caPEM, cert := selfSignedPair(t)
	caPath := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(caPath, caPEM, 0o600); err != nil {
		t.Fatalf("write ca: %v", err)
	}
	return fakeNATS{port: listen(t, true, cert), caPath: caPath}
}

func newPlaintextNATS(t *testing.T) fakeNATS {
	t.Helper()
	return fakeNATS{port: listen(t, false, tls.Certificate{})}
}

func listen(t *testing.T, secure bool, cert tls.Certificate) string {
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
			go speakNATS(conn, secure, cert)
		}
	}()

	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	return port
}

func speakNATS(conn net.Conn, secure bool, cert tls.Certificate) {
	defer func() { _ = conn.Close() }()

	required := "false"
	if secure {
		required = "true"
	}
	info := `INFO {"server_id":"TESTSERVER","server_name":"test","version":"2.14.3","proto":1,` +
		`"host":"127.0.0.1","port":4222,"headers":true,"max_payload":1048576,` +
		`"tls_required":` + required + "}\r\n"
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

// selfSignedPair returns a CA certificate in PEM form and a localhost leaf signed by it.
func selfSignedPair(t *testing.T) ([]byte, tls.Certificate) {
	t.Helper()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ca key: %v", err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "hermes-bootstrap-test-ca"},
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
