// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package observability

import (
	"context"
	"testing"

	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// The process-owner detector calls os/user.Current(), which in a CGO-disabled
// static binary falls back to $USER -- neither of which the distroless images
// provide. Requesting it there produced an error that crash-looped every
// service before any exporter was contacted.
//
// This asserts the attribute is not requested at all. It cannot reproduce the
// original failure, which needs CGO_ENABLED=0 and no $USER, so it guards the
// decision rather than the symptom: put resource.WithProcess() back and this
// fails.
func TestBuildResourceOmitsProcessOwner(t *testing.T) {
	res, err := buildResource(context.Background(), "test-service")
	if err != nil {
		t.Fatalf("buildResource: %v", err)
	}
	if res == nil {
		t.Fatal("buildResource returned a nil resource")
	}
	for _, attr := range res.Attributes() {
		if attr.Key == semconv.ProcessOwnerKey {
			t.Errorf("resource carries %s=%q; the owner detector cannot succeed in "+
				"a CGO-disabled image and must not be requested",
				attr.Key, attr.Value.AsString())
		}
	}
}

// Service name still lands, so dropping the owner detector did not take the
// rest of the resource with it.
func TestBuildResourceSetsServiceName(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "")
	res, err := buildResource(context.Background(), "hermes-test")
	if err != nil {
		t.Fatalf("buildResource: %v", err)
	}
	for _, attr := range res.Attributes() {
		if attr.Key == semconv.ServiceNameKey && attr.Value.AsString() == "hermes-test" {
			return
		}
	}
	t.Error("service.name not found in resource attributes")
}

// Init stays a no-op without an endpoint: local runs must not need the stack.
func TestInitNoopWithoutEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	shutdown, err := Init(context.Background(), "test-service")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}
