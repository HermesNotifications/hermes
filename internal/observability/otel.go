// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

// Package observability initializes OpenTelemetry for Hermes services.
//
// One call at process startup wires up TracerProvider, MeterProvider, and the
// W3C TraceContext propagator. Signals leave the process as OTLP/gRPC to the
// endpoint in OTEL_EXPORTER_OTLP_ENDPOINT (typically the in-cluster Collector).
//
// Init is a no-op when OTEL_EXPORTER_OTLP_ENDPOINT is empty — the same safety
// property dd-trace-go provided, so local runs without the stack don't fail.
package observability

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// ShutdownFunc flushes any buffered telemetry and releases resources.
// Call from main via defer after Init returns.
type ShutdownFunc func(context.Context) error

// Init configures global TracerProvider + MeterProvider + propagator and
// starts runtime metrics collection. Returns a shutdown func that flushes
// and releases exporters.
//
// If OTEL_EXPORTER_OTLP_ENDPOINT is unset, returns a no-op shutdown and
// leaves the global *providers* unchanged — safe for local runs without the
// observability stack. The propagator is installed either way; see below.
func Init(ctx context.Context, serviceName string) (ShutdownFunc, error) {
	// Before the endpoint check, deliberately. Propagation is independent of
	// export: a service that ships no telemetry of its own must still forward an
	// inbound traceparent, or it severs a trace its neighbours are recording.
	// Leaving this below the early return also left the no-op propagator
	// installed in every test binary, so Inject/Extract silently did nothing and
	// any test of the NATS carrier would have passed while proving nothing.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return noopShutdown, nil
	}

	res, err := buildResource(ctx, serviceName)
	if err != nil {
		// A detector that cannot answer should degrade the resource, not stop the
		// process. resource.New returns a usable resource alongside detector
		// errors, so the only fatal case is having nothing at all.
		//
		// This was not theoretical: with observability enabled, every service
		// crash-looped on "user: Current requires cgo or $USER set in
		// environment" -- raised before any exporter was contacted, so telemetry
		// setup failing closed took the entire service down with it.
		if res == nil {
			return nil, fmt.Errorf("observability: build resource: %w", err)
		}
		slog.WarnContext(ctx, "observability: partial resource, continuing",
			"error", err, "service", serviceName)
	}

	traceShutdown, err := initTraces(ctx, res)
	if err != nil {
		return nil, fmt.Errorf("observability: init traces: %w", err)
	}

	metricShutdown, err := initMetrics(ctx, res)
	if err != nil {
		// Tear down the trace provider we just installed before returning.
		_ = traceShutdown(ctx)
		return nil, fmt.Errorf("observability: init metrics: %w", err)
	}

	if err := runtime.Start(runtime.WithMinimumReadMemStatsInterval(15 * time.Second)); err != nil {
		_ = metricShutdown(ctx)
		_ = traceShutdown(ctx)
		return nil, fmt.Errorf("observability: start runtime instrumentation: %w", err)
	}

	return func(ctx context.Context) error {
		return errors.Join(traceShutdown(ctx), metricShutdown(ctx))
	}, nil
}

func buildResource(ctx context.Context, serviceName string) (*resource.Resource, error) {
	// WithProcess() minus WithProcessOwner(), spelled out rather than used
	// wholesale. The owner detector calls os/user.Current(), which in a
	// CGO-disabled static binary falls back to reading $USER -- and the
	// distroless images Hermes ships set neither. It can never succeed there, so
	// asking for it only produces an error to swallow. Every other process
	// attribute is kept.
	opts := []resource.Option{
		resource.WithFromEnv(),
		resource.WithHost(),
		resource.WithProcessPID(),
		resource.WithProcessExecutableName(),
		resource.WithProcessExecutablePath(),
		resource.WithProcessCommandArgs(),
		resource.WithProcessRuntimeName(),
		resource.WithProcessRuntimeVersion(),
		resource.WithProcessRuntimeDescription(),
		resource.WithTelemetrySDK(),
	}
	// Collect SDK defaults and env overrides first, then only default the
	// service name if neither OTEL_SERVICE_NAME nor service.name via
	// OTEL_RESOURCE_ATTRIBUTES already set it.
	if os.Getenv("OTEL_SERVICE_NAME") == "" {
		opts = append(opts, resource.WithAttributes(semconv.ServiceName(serviceName)))
	}
	return resource.New(ctx, opts...)
}

func initTraces(ctx context.Context, res *resource.Resource) (ShutdownFunc, error) {
	// OTLP/gRPC exporter pointing at OTEL_EXPORTER_OTLP_ENDPOINT. The exporter
	// reads the endpoint from env; we use insecure in-cluster by default (the
	// Collector's Service is plaintext inside the cluster).
	exp, err := otlptrace.New(ctx, otlptracegrpc.NewClient(
		otlptracegrpc.WithInsecure(),
	))
	if err != nil {
		return nil, err
	}

	// No WithSampler, on purpose. Passing one overrides OTEL_TRACES_SAMPLER, so
	// the hardcoded AlwaysSample() left no way to turn volume down short of a
	// code change and a redeploy -- and dispatch has been measured at ~7,900
	// msg/s. Omitting it lets the SDK read OTEL_TRACES_SAMPLER /
	// OTEL_TRACES_SAMPLER_ARG, whose default is parentbased_always_on: identical
	// behaviour to before, but now there is a dial.
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp,
			sdktrace.WithBatchTimeout(5*time.Second),
			sdktrace.WithMaxExportBatchSize(2048),
		),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}

func initMetrics(ctx context.Context, res *resource.Resource) (ShutdownFunc, error) {
	exp, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp,
			sdkmetric.WithInterval(30*time.Second),
		)),
	)
	otel.SetMeterProvider(mp)

	return mp.Shutdown, nil
}

func noopShutdown(context.Context) error { return nil }
