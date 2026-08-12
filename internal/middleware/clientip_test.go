// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func reqFrom(remoteAddr string, headers map[string][]string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = remoteAddr
	for k, vs := range headers {
		for _, v := range vs {
			r.Header.Add(k, v)
		}
	}
	return r
}

func mustProxies(t *testing.T, cidrs ...string) *TrustedProxies {
	t.Helper()
	tp, err := ParseTrustedProxies(cidrs)
	if err != nil {
		t.Fatalf("ParseTrustedProxies(%v): %v", cidrs, err)
	}
	return tp
}

// The whole point of the trust list: without it, the header is a free pass.
func TestClientIP_UntrustedPeerCannotSpoofForwardedFor(t *testing.T) {
	tp := mustProxies(t, "10.0.0.0/8")

	r := reqFrom("203.0.113.9:5555", map[string][]string{
		"X-Forwarded-For": {"1.2.3.4"},
		"X-Real-IP":       {"5.6.7.8"},
	})

	if got := tp.ClientIP(r); got != "203.0.113.9" {
		t.Errorf("expected the peer address to win, got %q", got)
	}
}

// The zero value must trust nothing, so a deployment that forgets to configure
// the list fails safe rather than open.
func TestClientIP_NoConfiguredProxiesTrustsNothing(t *testing.T) {
	for name, tp := range map[string]*TrustedProxies{
		"nil":   nil,
		"empty": mustProxies(t),
	} {
		t.Run(name, func(t *testing.T) {
			r := reqFrom("10.0.0.1:1234", map[string][]string{
				"X-Forwarded-For": {"1.2.3.4"},
			})
			if got := tp.ClientIP(r); got != "10.0.0.1" {
				t.Errorf("expected peer address, got %q", got)
			}
		})
	}
}

// A client that pre-seeds the header gets its own address appended by the proxy.
// Reading from the right skips the forged prefix.
func TestClientIP_TakesRightmostUntrustedHop(t *testing.T) {
	tp := mustProxies(t, "10.0.0.0/8")

	r := reqFrom("10.0.0.1:1234", map[string][]string{
		// "9.9.9.9" is what the attacker sent; "203.0.113.9" is what the edge saw.
		"X-Forwarded-For": {"9.9.9.9, 203.0.113.9, 10.0.0.5"},
	})

	if got := tp.ClientIP(r); got != "203.0.113.9" {
		t.Errorf("expected the rightmost untrusted hop, got %q", got)
	}
}

func TestClientIP_MultipleHeadersConcatenateInOrder(t *testing.T) {
	tp := mustProxies(t, "10.0.0.0/8")

	r := reqFrom("10.0.0.1:1234", map[string][]string{
		"X-Forwarded-For": {"9.9.9.9", "203.0.113.9, 10.0.0.5"},
	})

	if got := tp.ClientIP(r); got != "203.0.113.9" {
		t.Errorf("expected the rightmost untrusted hop across headers, got %q", got)
	}
}

func TestClientIP_FallsBackToPeerWhenEveryHopIsTrusted(t *testing.T) {
	tp := mustProxies(t, "10.0.0.0/8")

	r := reqFrom("10.0.0.1:1234", map[string][]string{
		"X-Forwarded-For": {"10.0.0.7, 10.0.0.5"},
	})

	if got := tp.ClientIP(r); got != "10.0.0.1" {
		t.Errorf("expected the peer address, got %q", got)
	}
}

func TestClientIP_RealIPOnlyConsultedWithoutAChain(t *testing.T) {
	tp := mustProxies(t, "10.0.0.0/8")

	withReal := reqFrom("10.0.0.1:1234", map[string][]string{
		"X-Real-IP": {"203.0.113.9"},
	})
	if got := tp.ClientIP(withReal); got != "203.0.113.9" {
		t.Errorf("expected X-Real-IP to be used, got %q", got)
	}

	// When both are present the chain wins, because it is the one that records
	// the whole path rather than a single hop's opinion.
	withBoth := reqFrom("10.0.0.1:1234", map[string][]string{
		"X-Forwarded-For": {"198.51.100.4"},
		"X-Real-IP":       {"203.0.113.9"},
	})
	if got := tp.ClientIP(withBoth); got != "198.51.100.4" {
		t.Errorf("expected the forwarded chain to win, got %q", got)
	}
}

// One client must not be able to hold several buckets by varying notation.
func TestClientIP_CanonicalisesAddresses(t *testing.T) {
	tp := mustProxies(t)

	cases := map[string]string{
		"1.2.3.4:9999":          "1.2.3.4",
		"[::ffff:1.2.3.4]:9999": "1.2.3.4",
		"1.2.3.4":               "1.2.3.4",
		"[2001:db8::1]:443":     "2001:db8::1",
		"[fe80::1%eth0]:443":    "fe80::1",
		"garbage":               "unknown",
	}
	for remote, want := range cases {
		if got := tp.ClientIP(reqFrom(remote, nil)); got != want {
			t.Errorf("RemoteAddr %q: expected %q, got %q", remote, want, got)
		}
	}
}

// A v4-mapped peer must match a plain v4 trust prefix, or the trust list
// silently stops working on a dual-stack listener.
func TestClientIP_TrustMatchesV4MappedPeer(t *testing.T) {
	tp := mustProxies(t, "10.0.0.0/8")

	r := reqFrom("[::ffff:10.0.0.1]:1234", map[string][]string{
		"X-Forwarded-For": {"203.0.113.9"},
	})

	if got := tp.ClientIP(r); got != "203.0.113.9" {
		t.Errorf("expected the v4-mapped peer to be trusted, got %q", got)
	}
}

func TestParseTrustedProxies(t *testing.T) {
	tp := mustProxies(t, "10.0.0.0/8", " 192.168.1.1 ", "", "2001:db8::/32")

	trusted := []string{"10.1.2.3:1", "192.168.1.1:1", "[2001:db8::5]:1"}
	for _, addr := range trusted {
		r := reqFrom(addr, map[string][]string{"X-Forwarded-For": {"203.0.113.9"}})
		if got := tp.ClientIP(r); got != "203.0.113.9" {
			t.Errorf("%s should be trusted, got %q", addr, got)
		}
	}

	// A neighbour of the single-address entry must not inherit its trust.
	r := reqFrom("192.168.1.2:1", map[string][]string{"X-Forwarded-For": {"203.0.113.9"}})
	if got := tp.ClientIP(r); got != "192.168.1.2" {
		t.Errorf("192.168.1.2 should not be trusted, got %q", got)
	}

	if _, err := ParseTrustedProxies([]string{"not-an-address"}); err == nil {
		t.Error("expected an error for a malformed entry")
	}
}
