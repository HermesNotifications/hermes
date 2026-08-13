// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

//go:build realtimecheck

// Command realtimecheck proves that a notification sent through the API arrives over the
// websocket, against a real deployment.
//
// # Why this exists as a separate thing
//
// A 101 Switching Protocols handshake proves the ingress route reaches Centrifugo. It proves
// nothing about delivery, and the difference is not academic: v0.1.0 answered 101 on every
// handshake while every publish from the inbox worker returned 404, because the chart pointed
// at Centrifugo's websocket port rather than its API port. The route was healthy and nothing
// was ever delivered.
//
// So this does the whole thing: mints a user token the way an integrator would, subscribes to
// that user's channel, sends a notification through /v1/send, and waits for the publication.
// Nothing short of that distinguishes "the endpoint answers" from "notifications arrive".
//
// Usage:
//
//	go run -tags realtimecheck ./tests/realtime \
//	  -api https://hermes.example.com -key "$HERMES_API_KEY" -org <uuid>
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/centrifugal/centrifuge-go"
)

// tlsConfig builds the client TLS settings.
//
// Prefer -cacert. A deployment behind an internal CA is the normal case for this chart, and
// the CA is readily available:
//
//	kubectl -n hermes get secret hermes-tls -o jsonpath='{.data.ca\.crt}' | base64 -d > ca.crt
//
// -insecure exists for a throwaway diagnosis and nothing else. It is worth being blunt about
// why: this tool's whole purpose is to answer "does delivery work", and skipping verification
// substitutes a weaker question for the real one on the transport under test.
func tlsConfig(caPath string, insecure bool) (*tls.Config, error) {
	if caPath != "" {
		pem, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("read CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("CA file %s contains no usable certificate", caPath)
		}
		return &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}, nil
	}
	if insecure {
		fmt.Fprintln(os.Stderr, "warning: -insecure skips TLS verification; prefer -cacert")
		return &tls.Config{InsecureSkipVerify: true}, nil //nolint:gosec // opt-in diagnostic only
	}
	return nil, nil
}

func main() {
	api := flag.String("api", "", "Hermes base URL, e.g. https://hermes.example.com")
	key := flag.String("key", os.Getenv("HERMES_API_KEY"), "Hermes API key")
	org := flag.String("org", "", "organization id (uuid)")
	user := flag.String("user", "realtime-check", "external user id")
	cacert := flag.String("cacert", "", "PEM file of the CA that signed the ingress certificate")
	insecure := flag.Bool("insecure", false, "skip TLS verification; prefer -cacert")
	timeout := flag.Duration("timeout", 90*time.Second, "how long to wait for the publication")
	flag.Parse()

	if *api == "" || *key == "" || *org == "" {
		fmt.Fprintln(os.Stderr, "-api, -key and -org are required")
		os.Exit(2)
	}

	tlsCfg, err := tlsConfig(*cacert, *insecure)
	if err != nil {
		fail("tls configuration", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	if tlsCfg != nil {
		client.Transport = &http.Transport{TLSClientConfig: tlsCfg}
	}

	// 1. Mint a user token, exactly as a backend integrating Hermes would.
	token, subject, err := mintToken(client, *api, *key, *org, *user)
	if err != nil {
		fail("mint user token", err)
	}
	channel := "user#" + subject
	fmt.Printf("token minted for %s (sub %s), channel %s\n", *user, subject, channel)

	// 2. Subscribe BEFORE sending, or the race is unwinnable: the publication is not retained,
	//    so a subscription established afterwards sees nothing and the result would be a false
	//    negative indistinguishable from a real failure.
	ws := centrifuge.NewJsonClient(wsURLFor(*api), centrifuge.Config{
		Token:     token,
		TLSConfig: tlsCfg,
	})
	defer ws.Close()

	connected := make(chan error, 1)
	ws.OnConnected(func(centrifuge.ConnectedEvent) { connected <- nil })
	ws.OnError(func(e centrifuge.ErrorEvent) {
		select {
		case connected <- fmt.Errorf("%s", e.Error):
		default:
		}
	})

	if err := ws.Connect(); err != nil {
		fail("connect websocket", err)
	}
	if err := waitFor(connected, 30*time.Second); err != nil {
		fail("websocket did not connect (can Centrifugo verify the token? it needs the same HERMES_JWT_SECRET)", err)
	}
	fmt.Println("websocket connected")

	arrived := make(chan []byte, 1)
	sub, err := ws.NewSubscription(channel)
	if err != nil {
		fail("create subscription", err)
	}
	sub.OnPublication(func(e centrifuge.PublicationEvent) {
		select {
		case arrived <- e.Data:
		default:
		}
	})
	subscribed := make(chan error, 1)
	sub.OnSubscribed(func(centrifuge.SubscribedEvent) { subscribed <- nil })
	sub.OnError(func(e centrifuge.SubscriptionErrorEvent) {
		select {
		case subscribed <- fmt.Errorf("%s", e.Error):
		default:
		}
	})
	if err := sub.Subscribe(); err != nil {
		fail("subscribe", err)
	}
	if err := waitFor(subscribed, 30*time.Second); err != nil {
		fail("could not subscribe to "+channel, err)
	}
	fmt.Println("subscribed to", channel)

	// 3. Send, through the real pipeline: Send -> NATS -> Dispatch -> inbox worker -> Centrifugo.
	title := fmt.Sprintf("realtime check %d", time.Now().UnixNano())
	if err := send(client, *api, *key, *org, *user, title); err != nil {
		fail("send notification", err)
	}
	fmt.Println("notification accepted, waiting for it to arrive over the websocket...")

	select {
	case data := <-arrived:
		fmt.Printf("\nDELIVERED over the websocket:\n  %s\n", string(data))
		if !bytes.Contains(data, []byte(title)) {
			fmt.Fprintf(os.Stderr, "\nwarning: a publication arrived but does not contain %q\n", title)
			os.Exit(1)
		}
		fmt.Println("\nOK: realtime delivery works end to end.")
	case <-time.After(*timeout):
		fmt.Fprintf(os.Stderr, `
FAILED: nothing arrived within %s.

The subscription was established before the send, so this is not a race. Check, in order:
  * the inbox worker's logs for a publish error -- a 404 means HERMES_CENTRIFUGO_API_URL
    points at Centrifugo's websocket port (8000) rather than its API port (9000);
  * a 401 there means Centrifugo's http_api.key is unset or does not match
    HERMES_CENTRIFUGO_API_KEY;
  * whether the notification reached the inbox at all (GET /v1/inbox) -- if it did, the
    pipeline is fine and only the publish leg is broken.
`, *timeout)
		os.Exit(1)
	}
}

func mintToken(c *http.Client, api, key, org, user string) (token, subject string, err error) {
	body, _ := json.Marshal(map[string]string{"user_id": user, "organization_id": org})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, api+"/v1/auth/token", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.Do(req)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = resp.Body.Close() }()
	payload, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("%d: %s", resp.StatusCode, payload)
	}

	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		return "", "", err
	}
	sub, err := subjectOf(out.Token)
	return out.Token, sub, err
}

// subjectOf reads the `sub` claim without verifying. The browser needs it for the channel name
// and has no other way to learn it; this mirrors what the client SDK does.
func subjectOf(jwt string) (string, error) {
	parts := bytes.Split([]byte(jwt), []byte("."))
	if len(parts) != 3 {
		return "", fmt.Errorf("not a JWT")
	}
	raw, err := base64URLDecode(string(parts[1]))
	if err != nil {
		return "", err
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		return "", err
	}
	if claims.Sub == "" {
		return "", fmt.Errorf("token has no sub claim")
	}
	return claims.Sub, nil
}

func base64URLDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

func send(c *http.Client, api, key, org, user, title string) error {
	body, _ := json.Marshal(map[string]any{
		"to":       map[string]string{"organization_id": org, "user_id": user},
		"content":  map[string]string{"title": title, "body": "sent by tests/realtime"},
		"channels": []string{"inbox"},
	})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, api+"/v1/send", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	// A HEADER, not a body field. The send API rejects unknown body properties outright, so
	// putting it in the body is a 422 rather than something quietly ignored.
	req.Header.Set("X-Idempotency-Key", fmt.Sprintf("realtime-check-%d", time.Now().UnixNano()))

	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	payload, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%d: %s", resp.StatusCode, payload)
	}
	return nil
}

// wsURLFor turns the API origin into the realtime websocket endpoint the ingress routes.
func wsURLFor(api string) string {
	url := api + "/realtime/connection/websocket"
	if len(api) > 8 && api[:8] == "https://" {
		return "wss://" + url[8:]
	}
	return "ws://" + url[7:]
}

func waitFor(ch chan error, d time.Duration) error {
	select {
	case err := <-ch:
		return err
	case <-time.After(d):
		return fmt.Errorf("timed out after %s", d)
	}
}

func fail(what string, err error) {
	fmt.Fprintf(os.Stderr, "FAILED: %s: %v\n", what, err)
	os.Exit(1)
}
