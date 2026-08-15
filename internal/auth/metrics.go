// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package auth

import (
	"context"

	"github.com/hermesnotifications/hermes/internal/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var meter = observability.Meter("github.com/hermesnotifications/hermes/internal/auth")

// authResults counts every authentication decision at both gates.
//
// Authentication was the one cross-cutting concern in the system emitting nothing. Its
// outcomes were visible only as 401s in the HTTP metrics, which cannot say why — and the
// why is the whole signal here, because the same status code covers a client that forgot
// a header, a key that was rotated out from under a caller, and someone working through a
// list of guesses.
//
// The two readings this supports:
//
//   - Operational. A deploy that ships a stale key turns a service's traffic into a solid
//     wall of invalid_key, and it looks exactly like an outage to the caller while every
//     Hermes health check stays green. Distinguishing that from an unreachable service
//     used to mean reading logs.
//   - Security. A credential-stuffing attempt is a rate of invalid_key against a flat
//     rate of ok, from the pod's perspective indistinguishable from success except in
//     this counter. This is the series to alert on for that; nothing else in Hermes can
//     see it.
//
// scheme is api_key|jwt and reason is a closed set fixed by the call sites in this
// package. Deliberately no organization or key identifier: an attacker chooses the values
// on failing requests, so labelling by them is an unbounded label an unauthenticated
// caller controls — the cardinality bomb in semantic-conventions.md, with the added
// property that anyone on the internet can set it off. Per-caller attribution belongs on
// the span.
var authResults, _ = meter.Int64Counter(
	"hermes.auth.result",
	metric.WithDescription("Authentication outcomes by scheme and reason."),
	metric.WithUnit("1"),
)

const (
	schemeAPIKey = "api_key"
	schemeJWT    = "jwt"

	reasonOK            = "ok"
	reasonMissing       = "missing_credential"
	reasonInvalidKey    = "invalid_key"
	reasonInvalidToken  = "invalid_token"
	reasonMissingClaims = "missing_claims"
	reasonNoSigningKeys = "no_signing_keys"
)

func recordAuthResult(ctx context.Context, scheme, reason string) {
	authResults.Add(ctx, 1, metric.WithAttributes(
		attribute.String("scheme", scheme),
		attribute.String("reason", reason),
	))
}
