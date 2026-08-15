// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package observability

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// The http.route problem for chi-routed services.
//
// otelhttp labels server spans and metrics with the *templated* route
// ("/v1/notifications/{id}"), never the raw path — that is the whole point of the
// label, and semantic-conventions.md forbids the raw path precisely because it
// carries IDs. otelhttp reads that template from http.Request.Pattern, which
// net/http's ServeMux assigns on the request it routes. So the ServeMux services
// (dispatch, the workers, prober) get the label for free.
//
// chi does not set Pattern, and the obvious fix — a middleware that reads
// chi.RouteContext and writes the answer somewhere — does not work on its own,
// because every layer in a net/http stack holds its own *http.Request. otelhttp is
// outermost; it records after the inner handler returns, using the request *it* was
// given. Anything an inner layer writes to its own request is invisible out there.
//
// The first attempt at this passed the route back up in a context-held pointer for a
// WithSpanNameFormatter to read. That could not work, and the reason is worth keeping:
// otelhttp names the span when it *starts* it, before routing has happened, and
// re-invokes the formatter afterwards only `if r.Pattern != ""` (handler.go:187 in
// contrib v0.69.0). For a chi service Pattern is always empty, so the second call never
// came and every span kept the name computed before there was a route to use — "POST",
// with no route anywhere on it. Verified in production on 0.2.0 before this fix.
//
// So ChiRoute does not ask otelhttp to name the span. It names it directly, along with
// the http.route attribute, at the one moment both the span and the pattern are in
// hand: inside otelhttp's span, immediately after the router has matched. Metrics still
// go through otelhttp's Labeler, which is read back on the way out and does work.

// ChiRoute records the route pattern chi matched, for services that route with chi
// instead of ServeMux. Wrap the chi router with it: it must be the layer directly
// outside the router so that the pattern is read immediately after routing.
//
// It supplies chi's RouteContext rather than letting chi allocate one, for two
// reasons. Reading the pattern requires holding the same RouteContext chi filled
// in, and chi's own is created on a request this layer never sees. It is also
// returned to a sync.Pool the moment Mux.ServeHTTP returns, so reading chi's copy
// afterwards would race whichever request picked it up next.
func ChiRoute(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rctx := chi.NewRouteContext()
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

		next.ServeHTTP(w, r)

		// Empty for a request that matched nothing — a 404 has no route, and
		// labelling it with the path it failed to match is the unbounded label
		// the route pattern exists to avoid.
		pattern := rctx.RoutePattern()
		if pattern == "" {
			return
		}

		// Metrics. otelhttp merges the labeler's attributes into the ones it records,
		// and it records after this returns.
		if labeler, ok := otelhttp.LabelerFromContext(r.Context()); ok {
			labeler.Add(attribute.String("http.route", pattern))
		}

		// Span name and route attribute, set directly rather than handed to otelhttp's
		// formatter — see the note at the top of this file for why the formatter never
		// gets a second chance on a chi route.
		//
		// The span here is otelhttp's own: it put it in the context on the way in, and
		// nothing between there and the router starts another. A no-op span (no
		// otelhttp in the chain, as in most handler tests) is not recording, so this
		// costs nothing and asserts nothing.
		if span := trace.SpanFromContext(r.Context()); span.IsRecording() {
			span.SetName(spanName(r.Method, pattern))
			span.SetAttributes(semconv.HTTPRoute(pattern))
		}
	})
}

// trimPattern mirrors otelhttp's own handling of ServeMux patterns, which may be
// "GET /v1/x" as well as "/v1/x". Returns "" for a pattern with no path.
func trimPattern(pattern string) string {
	if i := strings.IndexByte(pattern, '/'); i >= 0 {
		return pattern[i:]
	}
	return ""
}

// spanName is the "<METHOD> <route>" convention, with the bare method when there is
// no route to add.
func spanName(method, route string) string {
	method = strings.ToUpper(method)
	if route == "" {
		return method
	}
	return method + " " + route
}

// HTTPRouteSpanName names server spans "<METHOD> <route>" for the ServeMux services,
// where otelhttp does re-invoke the formatter once Request.Pattern is set.
//
// Pass it to otelhttp.WithSpanNameFormatter. It replaces a formatter that used
// r.URL.Path, which put notification IDs into span names — unbounded cardinality in
// Tempo, and against semantic-conventions.md. Chi services do not come through here
// for their final name; ChiRoute sets it directly.
func HTTPRouteSpanName(_ string, r *http.Request) string {
	return spanName(r.Method, trimPattern(r.Pattern))
}
