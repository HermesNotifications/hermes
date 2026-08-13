// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

//go:build integration

package cache

import (
	"os"
	"strings"
	"testing"
)

// The client half of the bundled-Redis TLS path, against a real TLS-only server holding a
// certificate from a private CA.
//
// The unit tests beside this cover the ways a CA bundle can be wrong. What they cannot cover is
// the case that matters: that supplying a correct one actually lets go-redis verify a server no
// public root vouches for. That is the whole reason cache.Options.CABundle exists, and the only
// way to be sure of it is to connect.
//
// Requires a TLS Redis and its CA:
//
//	HERMES_TEST_REDIS_TLS_URL=rediss://localhost:6390/0
//	HERMES_TEST_REDIS_CA=/path/to/ca.crt
func TestConnectsToATLSRedisUsingAPrivateCA(t *testing.T) {
	url := os.Getenv("HERMES_TEST_REDIS_TLS_URL")
	ca := os.Getenv("HERMES_TEST_REDIS_CA")
	if url == "" || ca == "" {
		t.Skip("set HERMES_TEST_REDIS_TLS_URL and HERMES_TEST_REDIS_CA to run")
	}

	client, err := ConnectWithOptions(url, Options{CABundle: ca})
	if err != nil {
		t.Fatalf("connect with CA bundle: %v", err)
	}
	t.Cleanup(client.Close)
}

// And the negative: without the bundle the same connection must FAIL. If it succeeded, the
// server would be being trusted for some other reason and the test above would be proving
// nothing at all.
func TestATLSRedisIsRejectedWithoutTheCA(t *testing.T) {
	url := os.Getenv("HERMES_TEST_REDIS_TLS_URL")
	if url == "" {
		t.Skip("set HERMES_TEST_REDIS_TLS_URL to run")
	}

	_, err := ConnectWithOptions(url, Options{})
	if err == nil {
		t.Fatal("connected without the CA; the server is trusted by something other than the bundle, so the positive test proves nothing")
	}
	if !strings.Contains(err.Error(), "certificate") {
		t.Errorf("expected a certificate verification failure, got: %v", err)
	}
}
