// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package send_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hermesnotifications/hermes/internal/observability"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// Reproduces bootstrap.ListenAndServe's wrapping exactly: real otelhttp outside the
// service handler, with HTTPRouteSpanName as the formatter. A test that assembles this
// differently proves nothing about production — which is the mistake that let spans
// named bare "POST" ship in 0.2.0.
func instrumentedLikeBootstrap(h http.Handler) http.Handler {
	return otelhttp.NewHandler(h, "http.server",
		otelhttp.WithSpanNameFormatter(observability.HTTPRouteSpanName),
	)
}

// The send API is served by huma over chi, and chi sets no Request.Pattern — so the
// route reaches the span only through observability.ChiRoute. 0.2.0 shipped with it not
// arriving: every span was named bare "POST", collapsing every endpoint under one name.
//
// Both spellings are covered because they register differently. /v1/send goes through
// the huma adapter; /healthz is put straight on the chi router in routes(). When this
// broke, both were wrong, which is what ruled huma out as the cause.
func TestServerSpanIsNamedWithTheRoute(t *testing.T) {
	for _, tc := range []struct {
		name, method, target, want string
		body                       string
	}{
		{name: "huma route", method: http.MethodPost, target: "/v1/send", want: "POST /v1/send", body: `{}`},
		{name: "plain chi route", method: http.MethodGet, target: "/healthz", want: "GET /healthz"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
			prev := otel.GetTracerProvider()
			otel.SetTracerProvider(tp)
			t.Cleanup(func() { otel.SetTracerProvider(prev) })

			srv := newTestServer(t)
			handler := instrumentedLikeBootstrap(srv.Handler())

			req := httptest.NewRequest(tc.method, tc.target, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			handler.ServeHTTP(httptest.NewRecorder(), req)

			spans := sr.Ended()
			if len(spans) == 0 {
				t.Fatal("no server span recorded")
			}
			last := spans[len(spans)-1]
			if got := last.Name(); got != tc.want {
				t.Errorf("server span name = %q, want %q\n"+
					"A bare method means the chi route pattern never reached the span.",
					got, tc.want)
			}

			var route string
			for _, attr := range last.Attributes() {
				if attr.Key == "http.route" {
					route = attr.Value.AsString()
				}
			}
			if route != tc.target {
				t.Errorf("http.route span attribute = %q, want %q; "+
					"without it traces cannot be filtered by endpoint", route, tc.target)
			}
		})
	}
}
