// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package centrifugo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type Client struct {
	apiURL     string
	apiKey     string
	httpClient *http.Client
}

// apiError is Centrifugo's logical error, which arrives inside a 200 response.
type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// apiResponse is the envelope every server-API reply uses: an `error` that is null on
// success, and a `result` (an empty object for publish). The result is kept raw and unread —
// its only use is proving the body came from Centrifugo at all, since exactly one of the two
// is always present.
type apiResponse struct {
	Error  *apiError       `json:"error"`
	Result json.RawMessage `json:"result"`
}

func NewClient(apiURL, apiKey string) *Client {
	return &Client{apiURL: apiURL, apiKey: apiKey, httpClient: &http.Client{
		Timeout: 5 * time.Second,
		// Unlike the webhook client, this one dials a fixed in-cluster Service, so
		// the default client metrics are bounded and stay on.
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}}
}

func (c *Client) Publish(ctx context.Context, channel string, data any) error {
	payload := map[string]any{"channel": channel, "data": data}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.apiURL+"/api/publish", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "apikey "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read centrifugo response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("centrifugo returned %d: %s", resp.StatusCode, string(respBody))
	}

	// The status code is not the whole answer. Centrifugo's server API reports *logical*
	// failures — unknown channel, bad permission, malformed request — as HTTP 200 with a
	// non-null `error` in the body; only transport-level problems produce a non-200, and then
	// only if the caller opts in with X-Centrifugo-Error-Mode. Checking the status alone
	// therefore treated a refused publish as a successful one: the delivery worker acked the
	// message, the notification was marked delivered, and it reached nobody. Silent, and
	// indistinguishable downstream from a message that was never sent.
	//
	// An empty body is treated as success. Real Centrifugo always sends the envelope, but a
	// proxy that strips it should not manufacture delivery failures.
	if len(bytes.TrimSpace(respBody)) == 0 {
		return nil
	}

	var parsed apiResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return fmt.Errorf("decode centrifugo response to publish on %s (%s): %w",
			channel, truncate(respBody), err)
	}
	if parsed.Error != nil {
		return fmt.Errorf("centrifugo refused publish to %s: %s (code %d)",
			channel, parsed.Error.Message, parsed.Error.Code)
	}
	// Parsing is not the same as recognising. A gateway answering `{}` or
	// `{"message":"upstream unavailable"}` unmarshals cleanly into a zero-valued envelope and
	// would be read as a delivered publish — the same silent drop the unparseable-body check
	// above rejects, only wearing valid JSON. Centrifugo v5 always sends one of the two
	// members (a successful publish is `{"result":{}}`, verified against the server), so a
	// body carrying neither did not come from Centrifugo.
	if parsed.Result == nil {
		return fmt.Errorf("centrifugo response to publish on %s carried neither result nor error: %s",
			channel, truncate(respBody))
	}
	return nil
}

// truncate bounds an unexpected body so a misrouted request cannot put an entire HTML error
// page into the logs.
func truncate(body []byte) string {
	const limit = 200
	if len(body) <= limit {
		return string(body)
	}
	return string(body[:limit]) + "…"
}
