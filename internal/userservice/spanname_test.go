// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package userservice_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hermesnotifications/hermes/internal/observability"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// The user service is chi-routed via huma and, like admin, never had
// observability.ChiRoute -- so every span was named for its method alone.
//
// It matters more here than anywhere else, because these are the routes that carry IDs.
// The templated route is the whole reason span names stopped using the raw path: without
// it the choice is a name that says nothing ("GET") or one that embeds a subscription ID
// and hands Tempo an unbounded set of span names. This asserts the template.
func TestServerSpanUsesTheTemplatedRoute(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	srv, _ := newTestServer(t)
	instrumented := otelhttp.NewHandler(srv.Handler(), "http.server",
		otelhttp.WithSpanNameFormatter(observability.HTTPRouteSpanName),
	)

	// A real subscription ID in the path. It must appear in neither the span name nor
	// the route attribute.
	req := httptest.NewRequest(http.MethodGet, "/v1/users/me/preferences", nil)
	instrumented.ServeHTTP(httptest.NewRecorder(), req)

	spans := sr.Ended()
	if len(spans) == 0 {
		t.Fatal("no server span recorded")
	}
	last := spans[len(spans)-1]

	const want = "GET /v1/users/me/preferences"
	if got := last.Name(); got != want {
		t.Errorf("server span name = %q, want %q", got, want)
	}

	var route string
	for _, attr := range last.Attributes() {
		if attr.Key == "http.route" {
			route = attr.Value.AsString()
		}
	}
	if route != "/v1/users/me/preferences" {
		t.Errorf("http.route span attribute = %q, want %q", route, "/v1/users/me/preferences")
	}
}

// The case the templated route exists for: a path carrying an ID must be reported by its
// pattern, never by what was requested.
func TestServerSpanDoesNotLeakThePathID(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	srv, _ := newTestServer(t)
	instrumented := otelhttp.NewHandler(srv.Handler(), "http.server",
		otelhttp.WithSpanNameFormatter(observability.HTTPRouteSpanName),
	)

	req := httptest.NewRequest(http.MethodPut, "/v1/users/me/preferences/sub_0123456789abcdef", nil)
	instrumented.ServeHTTP(httptest.NewRecorder(), req)

	spans := sr.Ended()
	if len(spans) == 0 {
		t.Fatal("no server span recorded")
	}
	last := spans[len(spans)-1]

	if name := last.Name(); name == "PUT /v1/users/me/preferences/sub_0123456789abcdef" {
		t.Fatalf("span name %q carries the subscription ID — unbounded cardinality in Tempo, "+
			"and the exact defect the templated route replaced", name)
	}
	const want = "PUT /v1/users/me/preferences/{subscription_id}"
	if got := last.Name(); got != want {
		t.Errorf("server span name = %q, want %q", got, want)
	}
}
