// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// These tests exist because the failure mode is silence. A broken route holder does not
// error or panic — it produces an empty http.route label, every series collapses into
// one, and the dashboards keep drawing a line. That is exactly how the label came to be
// missing in the first place.

// serveThrough builds the production arrangement: the holder outermost, then a stand-in
// for otelhttp, then the router. It returns the span name otelhttp would apply and the
// http.route it would record.
func serveThrough(t *testing.T, router http.Handler, chiRouted bool, target string) (spanName, metricRoute string) {
	t.Helper()

	inner := router
	if chiRouted {
		inner = ChiRoute(router)
	}

	// Stands in for otelhttp: it installs a Labeler, holds its own *http.Request, and
	// evaluates both on the way out — which is the whole reason this plumbing exists.
	otelStandIn := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(otelhttp.ContextWithLabeler(r.Context(), &otelhttp.Labeler{}))
		inner.ServeHTTP(w, r)
		spanName = HTTPRouteSpanName("http.server", r)
		metricRoute = routeForMetrics(r)
	})

	req := httptest.NewRequest(http.MethodGet, target, nil)
	WithHTTPRoute(otelStandIn).ServeHTTP(httptest.NewRecorder(), req)
	return spanName, metricRoute
}

func TestChiRouteReachesOuterMiddleware(t *testing.T) {
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

	spanName, metricRoute := serveThrough(t, mux, false, "/v1/things/abc123")

	// ServeMux assigns Request.Pattern on the request it routes, and that is the same
	// pointer the outer layer holds — so both work with no help from us. Nothing is
	// added to the Labeler here; otelhttp derives http.route from Pattern itself, and
	// adding it again would put the key in the attribute set twice.
	if want := "GET /v1/things/{id}"; spanName != want {
		t.Errorf("span name = %q, want %q", spanName, want)
	}
	if want := "/v1/things/{id}"; metricRoute != want {
		t.Errorf("http.route = %q, want %q", metricRoute, want)
	}
}

func TestRouteHooksTolerateAMissingHolder(t *testing.T) {
	// Any server that wires otelhttp without WithHTTPRoute — a test harness, a service
	// that builds its own stack — must degrade to no label rather than panic.
	req := httptest.NewRequest(http.MethodGet, "/v1/x", nil)

	if got := HTTPRouteSpanName("http.server", req); got != "GET" {
		t.Errorf("span name = %q, want %q", got, "GET")
	}
	if got := routeForMetrics(req); got != "" {
		t.Errorf("http.route = %q, want empty", got)
	}
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
