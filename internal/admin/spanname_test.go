// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package admin_test

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

// admin is chi-routed via huma, so the route reaches the span only through
// observability.ChiRoute -- and admin was the one chi service that never had it. Every
// admin span was named for its method alone, which nothing failed on and nothing
// surfaced: admin serves little traffic, so the collapsed names were never conspicuous.
//
// Mirrors the equivalent test in internal/send. The wrapping matches
// bootstrap.ListenAndServe, because a test that assembles it differently proves nothing
// about production -- the reason the original defect survived a passing suite.
func TestServerSpanIsNamedWithTheRoute(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	srv := newTestServer(t)
	instrumented := otelhttp.NewHandler(srv.Handler(), "http.server",
		otelhttp.WithSpanNameFormatter(observability.HTTPRouteSpanName),
	)

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/token", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	instrumented.ServeHTTP(httptest.NewRecorder(), req)

	spans := sr.Ended()
	if len(spans) == 0 {
		t.Fatal("no server span recorded")
	}
	last := spans[len(spans)-1]

	const want = "POST /v1/auth/token"
	if got := last.Name(); got != want {
		t.Errorf("server span name = %q, want %q\n"+
			"A bare method means the chi route never reached the span, so every admin "+
			"endpoint collapses under one name.", got, want)
	}

	var route string
	for _, attr := range last.Attributes() {
		if attr.Key == "http.route" {
			route = attr.Value.AsString()
		}
	}
	if route != "/v1/auth/token" {
		t.Errorf("http.route span attribute = %q, want %q; without it traces cannot be "+
			"filtered by endpoint", route, "/v1/auth/token")
	}
}
