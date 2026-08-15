// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// These tests exist because the failure mode is silence. A broken route hook does not
// error or panic — it produces spans named "GET" and an empty http.route label, every
// series collapses into one, and the dashboards keep drawing a line.
//
// They run against the REAL otelhttp handler, and that is the point rather than a
// detail. The previous version of this file used a stand-in that called the span-name
// formatter unconditionally on the way out. Real otelhttp calls it a second time only
// `if r.Pattern != ""` (handler.go:187 in contrib v0.69.0), which is never true for a
// chi route. So the stand-in was more forgiving than the thing it stood in for, the
// suite passed, and every chi service shipped spans named after nothing but the method.
// A fake that is kinder than production tests only itself.

// serveThrough builds the production arrangement — otelhttp outermost, then ChiRoute
// for the chi services, then the router — and reports the span name that was actually
// recorded plus the http.route the metrics would carry.
func serveThrough(t *testing.T, router http.Handler, chiRouted bool, target string) (spanName, metricRoute string) {
	t.Helper()

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	// The Labeler otelhttp installs is a pointer in the context, so a context captured
	// anywhere below it can be read after the request finishes — which is what makes the
	// attribute ChiRoute adds on the way out visible from here.
	var inner context.Context
	capture := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner = r.Context()
		router.ServeHTTP(w, r)
	})

	var h http.Handler = capture
	if chiRouted {
		h = ChiRoute(capture)
	}
	instrumented := otelhttp.NewHandler(h, "http.server",
		otelhttp.WithSpanNameFormatter(HTTPRouteSpanName),
	)

	req := httptest.NewRequest(http.MethodGet, target, nil)
	instrumented.ServeHTTP(httptest.NewRecorder(), req)

	spans := sr.Ended()
	if len(spans) == 0 {
		t.Fatal("no server span recorded")
	}
	spanName = spans[len(spans)-1].Name()

	if labeler, ok := otelhttp.LabelerFromContext(inner); ok {
		for _, kv := range labeler.Get() {
			if kv.Key == "http.route" {
				metricRoute = kv.Value.AsString()
			}
		}
	}
	return spanName, metricRoute
}

func TestChiRouteNamesTheSpanAndLabelsMetrics(t *testing.T) {
	router := chi.NewRouter()
	router.Get("/v1/notifications/{id}", func(w http.ResponseWriter, r *http.Request) {})

	spanName, metricRoute := serveThrough(t, router, true, "/v1/notifications/abc123")

	// The templated pattern, never the path that was actually requested. An ID in
	// either of these is the unbounded-cardinality bug this plumbing exists to avoid.
	if want := "GET /v1/notifications/{id}"; spanName != want {
		t.Errorf("span name = %q, want %q", spanName, want)
	}
	if want := "/v1/notifications/{id}"; metricRoute != want {
		t.Errorf("http.route = %q, want %q", metricRoute, want)
	}
}

// The route also has to land on the span as an attribute, not only in its name, or
// traces cannot be filtered by endpoint.
func TestChiRouteSetsTheRouteAttribute(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	router := chi.NewRouter()
	router.Get("/v1/notifications/{id}", func(w http.ResponseWriter, r *http.Request) {})
	instrumented := otelhttp.NewHandler(ChiRoute(router), "http.server",
		otelhttp.WithSpanNameFormatter(HTTPRouteSpanName),
	)
	instrumented.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/v1/notifications/abc123", nil))

	spans := sr.Ended()
	if len(spans) == 0 {
		t.Fatal("no server span recorded")
	}
	var got string
	for _, attr := range spans[len(spans)-1].Attributes() {
		if attr.Key == "http.route" {
			got = attr.Value.AsString()
		}
	}
	if want := "/v1/notifications/{id}"; got != want {
		t.Errorf("http.route span attribute = %q, want %q", got, want)
	}
}

func TestChiRouteUnmatchedRequestHasNoRoute(t *testing.T) {
	router := chi.NewRouter()
	router.Get("/v1/notifications/{id}", func(w http.ResponseWriter, r *http.Request) {})

	spanName, metricRoute := serveThrough(t, router, true, "/nope")

	// A 404 matched no route, and labelling it with the path it failed to match would
	// hand an unbounded label to anyone who can send a request.
	if spanName != "GET" {
		t.Errorf("span name = %q, want %q", spanName, "GET")
	}
	if metricRoute != "" {
		t.Errorf("http.route = %q, want empty for an unmatched request", metricRoute)
	}
}

func TestServeMuxRouteComesFromPattern(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/things/{id}", func(w http.ResponseWriter, r *http.Request) {})

	// No ChiRoute: ServeMux assigns Request.Pattern on the request it routes, which is
	// the one condition under which otelhttp re-invokes the formatter — so these
	// services need nothing from us, and otelhttp derives http.route for metrics itself
	// rather than through the Labeler.
	spanName, _ := serveThrough(t, mux, false, "/v1/things/abc123")

	if want := "GET /v1/things/{id}"; spanName != want {
		t.Errorf("span name = %q, want %q", spanName, want)
	}
}

func TestHTTPRouteSpanNameFallsBackToTheMethod(t *testing.T) {
	// A request that never reached a router — otelhttp names the span from this at
	// creation, before routing has happened, so it must degrade rather than panic.
	req := httptest.NewRequest(http.MethodGet, "/v1/x", nil)
	if got := HTTPRouteSpanName("http.server", req); got != "GET" {
		t.Errorf("span name = %q, want %q", got, "GET")
	}
}

func TestChiRouteWithoutOtelDoesNotPanic(t *testing.T) {
	// Most handler tests wire the router with no otelhttp above it, so the span in
	// context is a non-recording no-op and the Labeler is absent.
	router := chi.NewRouter()
	router.Get("/v1/x", func(w http.ResponseWriter, r *http.Request) {})
	ChiRoute(router).ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/v1/x", nil))
}

func TestTrimPattern(t *testing.T) {
	// otelhttp accepts both ServeMux spellings; mirror its own normalization.
	for _, tc := range []struct{ in, want string }{
		{"GET /v1/x", "/v1/x"},
		{"/v1/x", "/v1/x"},
		{"", ""},
		{"GET", ""},
	} {
		if got := trimPattern(tc.in); got != tc.want {
			t.Errorf("trimPattern(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
