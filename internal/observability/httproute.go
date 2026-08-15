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
)

// The http.route problem, and why it needs a holder rather than a middleware.
//
// otelhttp labels server spans and metrics with the *templated* route
// ("/v1/notifications/{id}"), never the raw path — that is the whole point of the
// label, and semantic-conventions.md forbids the raw path precisely because it
// carries IDs. otelhttp reads that template from http.Request.Pattern, which
// net/http's ServeMux assigns on the request it routes. So the ServeMux services
// (dispatch, the workers, prober) get the label for free.
//
// chi does not set Pattern, and the obvious fix — a middleware that reads
// chi.RouteContext and writes the answer somewhere — does not work, because every
// layer in a net/http stack holds its own *http.Request. otelhttp is outermost; it
// records metrics and renames the span after the inner handler returns, using the
// request *it* was given. Anything an inner layer writes to its own request or
// context is invisible from out there.
//
// What does cross the boundary is a pointer placed in the context on the way in, and
// there are two of them here for two different consumers:
//
//   - Metrics use otelhttp's own Labeler, which it installs in the context before
//     calling the next handler and reads back when it records. ChiRoute adds the route
//     to it; nothing else is needed.
//   - The span name has no equivalent, because otelhttp renames the span from a
//     formatter that receives only its own request. WithHTTPRoute installs a holder
//     outside otelhttp for that, and HTTPRouteSpanName reads it.
//
// So ChiRoute writes the same route to both. If that seems redundant, the alternative is
// spans named "GET" with no route on them at all.

// routeKey is the context key for the per-request route holder.
type routeKey struct{}

// routeHolder carries the matched route back up the middleware stack.
//
// No mutex: the write in ChiRoute happens-before the reads in HTTPRouteSpanName and
// HTTPRouteMetricAttributes, because all three run in the same goroutine and the
// write is sequenced by ServeHTTP returning.
type routeHolder struct{ pattern string }

// WithHTTPRoute installs the route holder. It must wrap the otelhttp handler, not
// sit inside it, so that the context otelhttp derives its own request from already
// carries the pointer.
func WithHTTPRoute(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		holder := &routeHolder{}
		ctx := context.WithValue(r.Context(), routeKey{}, holder)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

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

		// Span name, via the holder — see the note at the top of this file.
		if holder, _ := r.Context().Value(routeKey{}).(*routeHolder); holder != nil {
			holder.pattern = pattern
		}
	})
}

// routeFromContext returns the matched route, or "" if nothing matched or the
// holder was never installed.
func routeFromContext(ctx context.Context) string {
	holder, _ := ctx.Value(routeKey{}).(*routeHolder)
	if holder == nil {
		return ""
	}
	return holder.pattern
}

// trimPattern mirrors otelhttp's own handling of ServeMux patterns, which may be
// "GET /v1/x" as well as "/v1/x". Returns "" for a pattern with no path.
func trimPattern(pattern string) string {
	if i := strings.IndexByte(pattern, '/'); i >= 0 {
		return pattern[i:]
	}
	return ""
}

// HTTPRouteSpanName names server spans "<METHOD> <route>", falling back to the bare
// method when nothing matched.
//
// Pass it to otelhttp.WithSpanNameFormatter. It replaces a formatter that used
// r.URL.Path, which put notification IDs into span names — unbounded cardinality in
// Tempo, and against semantic-conventions.md. otelhttp's default would be correct
// for the ServeMux services on its own; this adds the chi ones.
func HTTPRouteSpanName(_ string, r *http.Request) string {
	route := routeFromContext(r.Context())
	if route == "" {
		route = trimPattern(r.Pattern)
	}
	method := strings.ToUpper(r.Method)
	if route == "" {
		return method
	}
	return method + " " + route
}

// routeForMetrics reports the http.route a recorded metric would carry, for tests. The
// production path adds it through otelhttp's Labeler in ChiRoute, and for ServeMux
// services otelhttp derives it from Request.Pattern with no help from us.
func routeForMetrics(r *http.Request) string {
	if labeler, ok := otelhttp.LabelerFromContext(r.Context()); ok {
		for _, kv := range labeler.Get() {
			if kv.Key == "http.route" {
				return kv.Value.AsString()
			}
		}
	}
	return trimPattern(r.Pattern)
}
