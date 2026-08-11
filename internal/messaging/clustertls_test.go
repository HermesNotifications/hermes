// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

//go:build integration

package messaging_test

import (
	"bufio"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hermes-notifications/hermes/internal/messaging"
)

// ADR 0005 phase 2. tls_test.go proves the client half against a hand-rolled TLS
// listener, which is enough for `make test` but proves nothing about the deployment: it
// does not exercise a cert-manager-issued certificate, the SAN list in
// deploy/k8s/base/infra/nats-certificates.yaml, or a NATS server that was configured from
// deploy/k8s/base/infra/nats-server.conf.
//
// This file closes that gap and is the reproducible form of the in-cluster verification.
// It skips unless pointed at a TLS-enabled NATS, so `make test-integration` against the
// plaintext `make infra-up` stack still compiles and passes:
//
//	kubectl -n <ns> port-forward svc/nats 14222:4222 &
//	kubectl -n <ns> get secret nats-server-tls -o jsonpath='{.data.ca\.crt}' | base64 -d > /tmp/ca.crt
//	HERMES_TLS_NATS_ADDR=127.0.0.1:14222 HERMES_TLS_NATS_CA=/tmp/ca.crt \
//	  go test -tags=integration ./internal/messaging/ -run TLSCluster -v
//
// Reaching a Service by port-forward means the TLS ServerName is "localhost", so the
// certificate under test needs a localhost SAN that the committed manifest deliberately
// does not carry. That is the one deviation; everything else is the deployed manifest.

func tlsClusterAddr(t *testing.T) string {
	t.Helper()
	addr := os.Getenv("HERMES_TLS_NATS_ADDR")
	if addr == "" {
		t.Skip("HERMES_TLS_NATS_ADDR unset; needs a TLS-enabled NATS (see file header)")
	}
	return addr
}

func tlsClusterCA(t *testing.T) string {
	t.Helper()
	ca := os.Getenv("HERMES_TLS_NATS_CA")
	if ca == "" {
		t.Skip("HERMES_TLS_NATS_CA unset; needs the issuing CA (see file header)")
	}
	return ca
}

// tlsClusterIdentity supplies the NKey credential ADR 0005 phase 3 added. Pointing
// HERMES_TLS_NATS_SEED_DIR at a directory of <service>.nk files — which is what
// `go run ./cmd/natskeys` produces and what the nats-nkeys Secret holds — makes these tests
// authenticate as a real service. Leaving it unset keeps them usable against a cluster that
// has TLS but no accounts yet, where WithIdentity's empty seed is a no-op.
func tlsClusterIdentity(t *testing.T, service string) messaging.Option {
	t.Helper()
	dir := os.Getenv("HERMES_TLS_NATS_SEED_DIR")
	if dir == "" {
		return messaging.WithIdentity(service, "")
	}
	return messaging.WithIdentity(service, filepath.Join(dir, service+".nk"))
}

// The whole path the services take: connect, declare the streams, publish — over TLS,
// against a certificate this repo's Certificate resource asked cert-manager to issue, as the
// NKey user whose permissions allow exactly this.
func TestTLSClusterConnectAndPublish(t *testing.T) {
	client, err := messaging.Connect("tls://"+tlsClusterAddr(t),
		messaging.WithCABundle(tlsClusterCA(t)),
		tlsClusterIdentity(t, "hermes-send"))
	if err != nil {
		t.Fatalf("tls:// with the issuing CA should connect: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.SetupStreams(ctx, messaging.StreamOptions{}); err != nil {
		t.Fatalf("SetupStreams over TLS: %v", err)
	}
	if err := client.Publish(ctx, "notification.send", []byte(`{"tls":true}`)); err != nil {
		t.Fatalf("Publish over TLS: %v", err)
	}
}

func TestTLSClusterRejectsClientWithoutCA(t *testing.T) {
	addr := tlsClusterAddr(t)
	_ = tlsClusterCA(t) // skip consistently with the other cases

	client, err := messaging.Connect("tls://" + addr)
	if err == nil {
		client.Close()
		t.Fatal("connected without the CA — the private CA must not be in the system trust store")
	}
	// Which x509 failure surfaces depends on which server nats.go reaches last: the
	// seed address fails as an unknown authority, and the cluster addresses NATS
	// advertises in connect_urls are bare pod IPs that fail the hostname check too.
	// Either way the connection is refused, which is the property under test.
	if !strings.Contains(err.Error(), "x509") {
		t.Fatalf("expected an x509 verification failure, got: %v", err)
	}
}

func TestTLSClusterRejectsPlaintextURL(t *testing.T) {
	addr := tlsClusterAddr(t)
	_ = tlsClusterCA(t)

	client, err := messaging.Connect("nats://" + addr)
	if err == nil {
		client.Close()
		t.Fatal("a plaintext URL produced a working client against a TLS-required server")
	}
}

// The server-side half of the question. A server that merely offers TLS would answer this
// plaintext CONNECT; one that requires it does not, and says so in its INFO.
func TestTLSClusterRefusesPlaintextProtocol(t *testing.T) {
	addr := tlsClusterAddr(t)
	_ = tlsClusterCA(t)

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}

	reader := bufio.NewReader(conn)
	info, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read INFO: %v", err)
	}
	if !strings.Contains(info, `"tls_required":true`) {
		t.Fatalf("server INFO does not require TLS: %s", info)
	}

	if _, err := conn.Write([]byte("CONNECT {\"verbose\":true}\r\nPING\r\n")); err != nil {
		t.Fatalf("write plaintext CONNECT: %v", err)
	}
	reply, err := reader.ReadString('\n')
	if err == nil && (strings.HasPrefix(reply, "PONG") || strings.HasPrefix(reply, "+OK")) {
		t.Fatalf("server answered a plaintext CONNECT with %q", strings.TrimSpace(reply))
	}
}
