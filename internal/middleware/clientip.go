// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package middleware

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// TrustedProxies decides whether a request's forwarding headers may be believed.
//
// This exists because per-IP rate limiting is only as trustworthy as the address
// it keys on. `X-Forwarded-For` is caller-supplied text: anyone can send one. If
// the limiter reads it unconditionally, an attacker sends a different value on
// every request and occupies a fresh bucket each time, which is worse than no
// limiter at all — it turns a bounded map into an attacker-controlled one.
//
// So the header is consulted only when the immediate peer is itself a proxy we
// put there. An empty TrustedProxies (the zero value, and the default) trusts
// nothing and always keys on the transport-level peer address.
type TrustedProxies struct {
	nets []netip.Prefix
}

// ParseTrustedProxies builds a TrustedProxies from CIDR strings.
//
// A bare IP is accepted and treated as a single-address prefix, since operators
// reasonably write "10.0.0.7" rather than "10.0.0.7/32".
func ParseTrustedProxies(cidrs []string) (*TrustedProxies, error) {
	tp := &TrustedProxies{}
	for _, raw := range cidrs {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if p, err := netip.ParsePrefix(s); err == nil {
			tp.nets = append(tp.nets, p.Masked())
			continue
		}
		addr, err := netip.ParseAddr(s)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q is neither a CIDR nor an IP address: %w", raw, err)
		}
		tp.nets = append(tp.nets, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return tp, nil
}

// trusts reports whether addr is one of the configured proxies.
func (tp *TrustedProxies) trusts(addr netip.Addr) bool {
	if tp == nil {
		return false
	}
	// Compare in one address family. A v4-mapped v6 address ("::ffff:10.0.0.1")
	// is the same host as its v4 form, but netip treats the two as unequal and a
	// v4 prefix does not contain the mapped form.
	addr = addr.Unmap()
	for _, n := range tp.nets {
		if n.Contains(addr) {
			return true
		}
	}
	return false
}

// ClientIP returns the address to attribute this request to.
//
// When the peer is trusted, the forwarded chain is walked from the RIGHT, and
// the first address that is not itself a trusted proxy wins. Taking the leftmost
// entry instead — the common shortcut — is forgeable: a client that sends
// "X-Forwarded-For: 1.2.3.4" has its own address appended by the proxy, so the
// leftmost value is whatever the attacker chose. The rightmost untrusted hop is
// the closest address the infrastructure actually observed.
//
// The result is a bare IP with no port, canonicalised, so that one client cannot
// occupy several buckets by varying source port or v4-mapped notation.
func (tp *TrustedProxies) ClientIP(r *http.Request) string {
	peer := peerAddr(r.RemoteAddr)
	if !tp.trusts(peer) {
		return addrKey(peer)
	}

	for _, hop := range forwardedChain(r) {
		addr, err := netip.ParseAddr(strings.TrimSpace(hop))
		if err != nil {
			continue
		}
		if !tp.trusts(addr) {
			return addrKey(addr)
		}
	}

	// Every hop was a proxy we trust, or there was no usable header. Falling back
	// to the peer keeps a bucket that is at least real; returning "" here would
	// collapse all such traffic into one shared bucket.
	return addrKey(peer)
}

// forwardedChain returns the forwarded hops, rightmost first.
func forwardedChain(r *http.Request) []string {
	var hops []string
	// A request may carry several X-Forwarded-For headers; net/http keeps them
	// separate. Semantically they concatenate in order, so flatten before
	// reversing rather than reversing each header on its own.
	for _, h := range r.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(h, ",")...)
	}

	for i, j := 0, len(hops)-1; i < j; i, j = i+1, j-1 {
		hops[i], hops[j] = hops[j], hops[i]
	}

	// X-Real-IP is a single value set by the proxy itself, so it is only worth
	// consulting when there is no chain to walk.
	if len(hops) == 0 {
		if v := strings.TrimSpace(r.Header.Get("X-Real-IP")); v != "" {
			hops = append(hops, v)
		}
	}
	return hops
}

// peerAddr extracts the address from a "host:port" RemoteAddr.
func peerAddr(remoteAddr string) netip.Addr {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// httptest and some transports hand over a bare address.
		host = remoteAddr
	}
	// A v6 literal may still arrive bracketed, and may carry a zone.
	host = strings.Trim(host, "[]")
	if i := strings.IndexByte(host, '%'); i >= 0 {
		host = host[:i]
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return addr.Unmap()
}

// addrKey renders an address as a bucket key.
func addrKey(addr netip.Addr) string {
	if !addr.IsValid() {
		// Unparseable peer. One shared bucket for the unattributable is the
		// conservative choice: it cannot be used to escape a limit, only to
		// contend with others in the same state.
		return "unknown"
	}
	return addr.Unmap().String()
}
