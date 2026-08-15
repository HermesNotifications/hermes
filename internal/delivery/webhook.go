// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
)

type WebhookProvider struct {
	name   string
	url    string
	client *http.Client
}

func NewWebhookProvider(name, url string) *WebhookProvider {
	return &WebhookProvider{
		name: name,
		url:  url,
		client: &http.Client{
			Timeout: 10 * time.Second,
			// Carries the delivery's trace context to the endpoint and records the
			// call as a client span, so a trace no longer stops at the worker.
			//
			// Metrics are deliberately suppressed. otelhttp labels its client
			// metrics with server.address, and this client dials whatever URL a
			// customer configured -- an unbounded label, which
			// docs/observability/semantic-conventions.md forbids outright. Spans
			// carry the same information and Tempo is built for that cardinality;
			// a Prometheus series per customer host is not.
			Transport: otelhttp.NewTransport(http.DefaultTransport,
				otelhttp.WithMeterProvider(noopmetric.NewMeterProvider()),
			),
		},
	}
}

func (w *WebhookProvider) Name() string {
	return w.name
}

func (w *WebhookProvider) Send(ctx context.Context, req DeliveryRequest) (DeliveryResult, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return DeliveryResult{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return DeliveryResult{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := w.client.Do(httpReq)
	if err != nil {
		return DeliveryResult{}, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return DeliveryResult{
			Success:      false,
			ProviderName: w.name,
			Error:        fmt.Sprintf("unexpected status code: %d", resp.StatusCode),
		}, fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return DeliveryResult{
		Success:      true,
		ProviderName: w.name,
		ProviderID:   resp.Header.Get("X-Provider-ID"),
	}, nil
}
