## 1. Goal

Add optional SigNoz telemetry export alongside the existing Prometheus/LGTM observability stack, preserving Prometheus for metrics scraping, alerting, dashboards, and long-term multi-backend support.

## 2. Approach

Keep Prometheus. The repository already made OpenTelemetry the app-side contract in `internal/observability/otel.go:44-139` and the OTel Collector the single fan-out point in `docs/observability/adr/002-otel-collector-fan-out.md:31-36`, so SigNoz should be added as another collector exporter, not as a second SDK and not by replacing Prometheus. Dropping Prometheus would be a larger migration because it currently owns remote-write metrics ingest in `deploy/observability/base/otel-collector/values.yaml:97-100`, exporter scraping in `docs/observability/architecture.md:49-51`, alert rules in `deploy/observability/base/prometheus-rules/*.yaml`, and Tempo metrics-generator output in `deploy/observability/base/tempo/values.yaml:19-22`.

This plan treats SigNoz as an optional external or separately installed OTLP backend. Installing/operating SigNoz and ClickHouse in this repo would be a separate plan because `docs/observability/adr/001-lgtm-over-signoz.md:21-25` explicitly calls out ClickHouse/SigNoz operational weight as the reason LGTM remains primary.

## 3. File Changes

- **Modify** `deploy/observability/base/otel-collector/values.yaml`: Add a disabled-by-default SigNoz OTLP exporter shape and document the multi-backend fan-out. Keep base pipelines exporting only to `otlp/tempo`, `prometheusremotewrite`, and `debug` so local LGTM behavior is unchanged.
- **Create** `deploy/observability/overlays/local/patches/otel-collector-signoz-exporter.yaml`: Local opt-in patch that adds `otlp/signoz` exporter and appends it to traces, metrics, and logs pipelines for users pointing to an existing SigNoz endpoint.
- **Create** `deploy/observability/overlays/staging/patches/otel-collector-signoz-exporter.yaml`: Staging opt-in patch that adds `otlp/signoz` to collector pipelines with endpoint and TLS/header configuration sourced from environment variables or Secret-backed env vars.
- **Create** `deploy/observability/overlays/production/patches/otel-collector-signoz-exporter.yaml`: Production equivalent of the staging patch, kept separate so rollout can be enabled independently per environment.
- **Modify** `deploy/observability/overlays/local/kustomization.yaml`: Add the SigNoz patch only if using an explicit local SigNoz overlay variant, or create a sibling local overlay if Kustomize conditionality would make the default local stack unexpectedly send telemetry elsewhere.
- **Modify** `deploy/observability/overlays/staging/kustomization.yaml`: Include the SigNoz collector patch when staging should dual-emit to SigNoz.
- **Modify** `deploy/observability/overlays/production/kustomization.yaml`: Include the SigNoz collector patch only after staging validation; production remains opt-in.
- **Create** `deploy/observability/overlays/staging/signoz-external-secret.yaml`: Optional ExternalSecret for SigNoz ingest headers/API key when the target SigNoz deployment requires authentication.
- **Create** `deploy/observability/overlays/production/signoz-external-secret.yaml`: Production counterpart for SigNoz auth material.
- **Modify** `Tiltfile`: Add an optional `signoz` boolean flag if local dual-emit should be available through Tilt, following the existing `observability` and `datadog` flags in `Tiltfile:4-8` and resource wiring in `Tiltfile:187-269`.
- **Modify** `charts/hermes/values.yaml`: Expand `observability` from `enabled/provider` at `charts/hermes/values.yaml:319-323` to a multi-backend model, for example `observability.otel.endpoint`, `observability.serviceMonitor.enabled`, and `observability.backends.signoz.enabled/endpoint/headersSecret`.
- **Modify** `charts/hermes/values.schema.json`: Update the schema currently limited to `provider: otel|datadog` at `charts/hermes/values.schema.json:83-94` so chart users can configure OTLP export and SigNoz without pretending SigNoz is a mutually exclusive provider.
- **Modify** `charts/hermes/templates/configmap.yaml`: Add `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_PROTOCOL`, and optional `OTEL_RESOURCE_ATTRIBUTES` rendering from Helm values; the Kustomize stack already sets these globally in `deploy/k8s/base/kustomization.yaml:20-29`, but the chart does not.
- **Modify** `charts/hermes/templates/services/*.yaml`: Add per-service `OTEL_SERVICE_NAME=hermes-<service>` env vars in the Helm templates, matching Kustomize service manifests such as `deploy/k8s/base/services/send.yaml:40-46`.
- **Modify** `charts/hermes/templates/servicemonitor.yaml`: Stop tying ServiceMonitor creation to `provider == "otel"` at `charts/hermes/templates/servicemonitor.yaml:1`; gate it on `observability.serviceMonitor.enabled` so Prometheus scraping can coexist with SigNoz export.
- **Modify** `docs/observability/adr/001-lgtm-over-signoz.md`: Amend the ADR status/date and clarify that LGTM remains the primary in-cluster stack while SigNoz is supported as an optional OTel backend. Preserve the current rationale in `docs/observability/adr/001-lgtm-over-signoz.md:19-35`.
- **Modify** `docs/observability/adr/002-otel-collector-fan-out.md`: Add a short note that SigNoz is now a concrete example of the fan-out design described in `docs/observability/adr/002-otel-collector-fan-out.md:31-36`.
- **Modify** `docs/observability/README.md`: Update the one-paragraph overview at `docs/observability/README.md:21-28` to mention optional SigNoz dual-export.
- **Modify** `docs/observability/architecture.md`: Update the component table and data-flow diagram at `docs/observability/architecture.md:7-56` to show optional `OTel Collector -> SigNoz` while Prometheus remains the metrics/alerts plane.
- **Modify** `docs/observability/operations.md`: Add rollout and troubleshooting steps for SigNoz exporter failures next to existing collector debugging in `docs/observability/operations.md:66-98`.
- **Modify** `docs/observability/local-dev.md`: Document local SigNoz dual-emit usage and port-forward expectations next to the current observability flow in `docs/observability/local-dev.md:5-52`.
- **Modify** `docs/self-hosting/configuration.md`: Replace the mismatched `hermes.otel` / `serviceMonitor` examples at `docs/self-hosting/configuration.md:90-122` with the actual chart values and add a SigNoz example.

## 4. Implementation Steps

### Task 1: Preserve Prometheus as canonical and document the decision

1. Amend `docs/observability/adr/001-lgtm-over-signoz.md:3-35` to set status to `Accepted; amended 2026-06-13` and add a short subsection: SigNoz is supported as an optional OTLP destination, not as the primary replacement for LGTM/Prometheus.
2. Amend `docs/observability/adr/002-otel-collector-fan-out.md:31-36` to mention SigNoz as a supported collector fan-out backend, reinforcing that no service code changes are required.
3. Update `docs/observability/README.md:21-28` so the overview says the collector fans out to Tempo, Prometheus, Datadog during Phase 1, and optionally SigNoz when configured.
4. Update `docs/observability/architecture.md:7-17` and `docs/observability/architecture.md:20-56` to add optional SigNoz in the topology while leaving Prometheus scrape paths and Grafana links intact.

### Task 2: Add SigNoz collector export without changing app code

1. In `deploy/observability/base/otel-collector/values.yaml:1-12`, update comments to describe Prometheus/LGTM plus optional SigNoz fan-out.
2. In `deploy/observability/base/otel-collector/values.yaml:92-108`, add a commented or documented `otlp/signoz` exporter pattern using endpoint, TLS, and optional headers, but do not add it to base pipelines by default.
3. Create `deploy/observability/overlays/staging/patches/otel-collector-signoz-exporter.yaml` to patch the collector ConfigMap or Helm-rendered config with an `otlp/signoz` exporter and append it to `service.pipelines.traces.exporters`, `service.pipelines.metrics.exporters`, and `service.pipelines.logs.exporters`.
4. Create `deploy/observability/overlays/production/patches/otel-collector-signoz-exporter.yaml` with the same structure as staging but production Secret names and endpoint values.
5. Create `deploy/observability/overlays/local/patches/otel-collector-signoz-exporter.yaml` for local testing against an existing SigNoz endpoint, keeping the default `deploy/observability/overlays/local/kustomization.yaml:4-31` unchanged unless the `Tiltfile` flag is enabled.
6. Add `signoz-external-secret.yaml` in staging and production if SigNoz auth headers/API keys are required, mirroring the current Datadog secret injection pattern in `deploy/observability/overlays/staging/external-secrets.yaml` and `deploy/observability/overlays/staging/patches/otel-collector-dd-exporter.yaml:10-21`.

### Task 3: Make local and environment rollout explicit

1. In `Tiltfile:4-8`, add `config.define_bool("signoz", usage="Enable SigNoz OTel Collector export to an existing SigNoz endpoint")`.
2. In `Tiltfile:207-269`, when both `observability_enabled` and `signoz_enabled` are true, render a SigNoz-enabled overlay or include the local SigNoz patch before applying objects.
3. Keep `tilt up -- --observability` behavior unchanged; require `tilt up -- --observability --signoz` for dual-export.
4. Update `deploy/observability/overlays/staging/kustomization.yaml:8-13` and `deploy/observability/overlays/production/kustomization.yaml:9-17` to include SigNoz patches only after secrets/endpoints are available.

### Task 4: Fix Helm chart observability values for multi-backend support

1. In `charts/hermes/values.yaml:319-323`, replace `provider: "otel"` with structured values such as:
   - `observability.enabled`
   - `observability.otel.endpoint`
   - `observability.otel.protocol`
   - `observability.resourceAttributes`
   - `observability.serviceMonitor.enabled`
   - `observability.backends.signoz.enabled`
   - `observability.backends.signoz.endpoint`
   - `observability.backends.signoz.headersSecret`
2. In `charts/hermes/values.schema.json:83-94`, validate the new structure, including endpoint strings, protocol enum `grpc|http/protobuf`, and object shape for optional headers secret references.
3. In `charts/hermes/templates/configmap.yaml:7-37`, render `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_PROTOCOL`, and `OTEL_RESOURCE_ATTRIBUTES` when `observability.enabled` is true.
4. In each `charts/hermes/templates/services/*.yaml`, add the appropriate `OTEL_SERVICE_NAME` env var for that service, matching Kustomize’s `hermes-send` convention in `deploy/k8s/base/services/send.yaml:43-46`.
5. In `charts/hermes/templates/servicemonitor.yaml:1-21`, gate ServiceMonitor output on `observability.serviceMonitor.enabled`, not on a mutually exclusive provider value.

### Task 5: Update operations and self-hosting docs

1. In `docs/observability/operations.md:66-98`, add SigNoz troubleshooting: check `otelcol_exporter_send_failed_*{exporter="otlp/signoz"}`, collector logs, endpoint DNS, TLS mode, and auth headers.
2. In `docs/observability/local-dev.md:33-52`, add local verification: trigger a service request, confirm the same trace appears in Tempo and SigNoz, and confirm Prometheus metrics still populate.
3. In `docs/self-hosting/configuration.md:90-122`, replace stale examples with actual chart values for OpenTelemetry, Prometheus ServiceMonitor, and SigNoz.
4. Add an explicit warning in `docs/self-hosting/configuration.md:90-122` that enabling SigNoz does not remove Prometheus alerts or ServiceMonitor scraping; users who want SigNoz-only operation need a later Prometheus replacement plan.

## 5. Acceptance Criteria

1. With no SigNoz values or overlays enabled, rendered `deploy/observability/overlays/local` collector pipelines remain unchanged: traces export to `otlp/tempo`, metrics export to `prometheusremotewrite`, logs export to `debug`, matching `deploy/observability/base/otel-collector/values.yaml:118-130` behavior.
2. With the SigNoz overlay enabled, the rendered OTel Collector config contains exactly one `otlp/signoz` exporter and includes `otlp/signoz` in traces, metrics, and logs pipeline exporter lists.
3. Enabling SigNoz does not remove `prometheusremotewrite` from the metrics pipeline in `deploy/observability/base/otel-collector/values.yaml:122-125`.
4. Enabling SigNoz does not remove kube-prometheus-stack, ServiceMonitor resources, PrometheusRule resources, or Tempo metrics-generator remote write from `deploy/observability/base/tempo/values.yaml:19-22`.
5. `tilt up -- --observability` continues to render the existing LGTM stack only; SigNoz fan-out requires an explicit opt-in flag or overlay.
6. Helm rendering with `observability.enabled=true` sets `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_PROTOCOL`, and per-service `OTEL_SERVICE_NAME` env vars for all 9 long-running services.
7. Helm rendering with Prometheus ServiceMonitor enabled emits 9 ServiceMonitor resources, matching the service list currently in `charts/hermes/templates/servicemonitor.yaml:4`.
8. Helm rendering with SigNoz enabled and ServiceMonitor enabled supports both simultaneously; schema validation does not require choosing one provider.
9. Docs state clearly that SigNoz support is additive and Prometheus remains required for current alert rules in `deploy/observability/base/prometheus-rules/service.rules.yaml`, `pipeline.rules.yaml`, and `infra.rules.yaml`.
10. No Go application code changes are required; telemetry remains initialized through `internal/bootstrap/serve.go:27-53` and `internal/observability/otel.go:44-139`.

## 6. Verification Steps

1. Validate YAML/JSON syntax:
   - `jq . charts/hermes/values.schema.json > /dev/null`
   - `kubectl apply --dry-run=client -f deploy/observability/base/prometheus-rules/service.rules.yaml`
   - `kubectl apply --dry-run=client -f deploy/observability/base/prometheus-rules/pipeline.rules.yaml`
   - `kubectl apply --dry-run=client -f deploy/observability/base/prometheus-rules/infra.rules.yaml`
2. Validate Helm chart rendering:
   - `make helm-lint`
   - `helm template test charts/hermes/ --set hermes.jwt.secret=test --set hermes.apiKey.hmacSecret=test --set global.domain=test.example.com --set observability.enabled=true > /tmp/hermes.yaml`
   - Confirm `/tmp/hermes.yaml` contains `OTEL_EXPORTER_OTLP_ENDPOINT`, 9 `OTEL_SERVICE_NAME` entries, and the expected ServiceMonitor count when enabled.
3. Validate Kustomize rendering:
   - `kubectl kustomize deploy/k8s/overlays/local`
   - `kubectl kustomize deploy/k8s/overlays/staging`
   - `kubectl kustomize deploy/k8s/overlays/production`
   - `kubectl kustomize --enable-helm deploy/observability/overlays/local`
   - `kubectl kustomize --enable-helm deploy/observability/overlays/staging`
   - `kubectl kustomize --enable-helm deploy/observability/overlays/production`
4. Local manual verification with SigNoz endpoint available:
   - Start `tilt up -- --observability --signoz`.
   - Send a request through the Send service.
   - In Grafana Tempo, confirm a trace for `service.name=hermes-send` appears within 30 seconds.
   - In SigNoz, confirm the same service and request trace appears within 30 seconds.
   - In Grafana Prometheus, confirm `http_server_request_duration_seconds_count{service="hermes-send"}` increments.
5. Failure-path verification:
   - Configure an invalid SigNoz endpoint in local mode.
   - Confirm Hermes pods stay healthy because apps still emit only to the collector.
   - Confirm collector self-metrics/logs show exporter failures for `otlp/signoz`, while Prometheus and Tempo export continue.

## 7. Risks & Mitigations

- **Risk: Collector config patching becomes brittle.** Existing Datadog comments in `deploy/observability/overlays/staging/patches/otel-collector-dd-exporter.yaml:1-8` already hint at unclear pipeline override mechanics. Mitigation: implement SigNoz through explicit rendered collector values or a focused ConfigMap patch with verification that the final rendered config contains the intended exporter lists.
- **Risk: Dual-export increases collector CPU/memory and queue pressure.** `deploy/observability/base/otel-collector/values.yaml:17-23` currently requests 200m CPU and 512Mi memory. Mitigation: monitor collector `otelcol_exporter_*` and `otelcol_processor_refused_*` metrics after enabling SigNoz; add staging/production resource patches if refused spans or send failures increase.
- **Risk: Metric cardinality costs can hit both Prometheus and SigNoz.** The current collector strips `user_id`, `notification_id`, and `organization_id` before Prometheus in `deploy/observability/base/otel-collector/values.yaml:82-90`; if SigNoz receives metrics before that processor, it may ingest high-cardinality labels. Mitigation: keep SigNoz metrics in the same metrics pipeline after `attributes/metrics`, or add a SigNoz-specific metrics processor with the same deletions.
- **Risk: Operators may assume SigNoz replaces existing alerts.** Alerting currently depends on PrometheusRule resources in `deploy/observability/base/prometheus-rules/*.yaml`. Mitigation: docs and ADR updates must state Prometheus remains required until a separate alerting migration plan exists.
- **Risk: Helm chart observability values diverge from Kustomize.** The chart currently lacks OTEL env rendering even though Kustomize sets it in `deploy/k8s/base/kustomization.yaml:20-29`. Mitigation: add Helm rendering tests and keep examples in `docs/self-hosting/configuration.md:90-122` tied to actual `charts/hermes/values.yaml` keys.