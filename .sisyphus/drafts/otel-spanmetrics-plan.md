# Plan: Roundtable OTEL Spanmetrics Pipeline

## Goal

Route roundtable trace spans through Alloy's `otelcol.connector.spanmetrics` to generate Prometheus metrics, then build a Grafana dashboard showing invocation count, success/error rates, latency percentiles, role distribution, and session correlation.

## Current State

- Roundtable emits OTEL trace spans to `$OTEL_EXPORTER_OTLP_ENDPOINT/v1/traces` (HTTP JSON)
- Containers send to `http://100.80.215.11:30318` → NodePort → Alloy port 4318
- Alloy has `otelcol.receiver.otlp` on ports 4317 (gRPC) + 4318 (HTTP)
- Alloy's OTLP output block routes `metrics` and `logs` but **NOT traces** — spans are silently dropped
- Alloy forwards metrics to Prometheus via `prometheus.remote_write` at `http://prometheus:9090/api/v1/write`
- Grafana at `https://brahma.myth-gecko.ts.net:8443` with Prometheus datasource

## Span Attributes (emitted by roundtable)

Resource attributes:
- `service.name` = "roundtable"
- `project.name`, `session.tag`, `host.name` (from OTEL_RESOURCE_ATTRIBUTES)

Span attributes:
- `roundtable.role` (string: default, planner, codereviewer)
- `roundtable.gemini.role` (string)
- `roundtable.codex.role` (string)
- `roundtable.gemini.status` (string: ok, error, timeout, not_found, probe_failed)
- `roundtable.codex.status` (string)
- `roundtable.gemini.elapsed_ms` (int)
- `roundtable.codex.elapsed_ms` (int)
- `roundtable.total_elapsed_ms` (int)
- `roundtable.files_count` (int)
- `roundtable.gemini.model` (string)
- `roundtable.codex.model` (string)

Span name: `roundtable.invoke`
Span kind: 3 (CLIENT)

## Changes Required

### 1. Alloy Config — Add spanmetrics connector (brahma)

Add `traces` output to existing `otelcol.receiver.otlp` and a new `otelcol.connector.spanmetrics` block.

**Current receiver output:**
```river
output {
  metrics = [otelcol.processor.batch.default.input]
  logs    = [otelcol.processor.batch.default.input]
}
```

**New receiver output:**
```river
output {
  metrics = [otelcol.processor.batch.default.input]
  logs    = [otelcol.processor.batch.default.input]
  traces  = [otelcol.connector.spanmetrics.roundtable.input]
}
```

**New spanmetrics connector:**
```river
otelcol.connector.spanmetrics "roundtable" {
  namespace = "roundtable"

  dimension {
    name = "roundtable.role"
    default = "default"
  }
  dimension {
    name = "roundtable.gemini.status"
    default = "unknown"
  }
  dimension {
    name = "roundtable.codex.status"
    default = "unknown"
  }
  dimension {
    name = "roundtable.gemini.model"
    default = "unknown"
  }
  dimension {
    name = "roundtable.codex.model"
    default = "unknown"
  }

  histogram {
    unit = "ms"
    explicit {
      buckets = ["500ms", "1s", "5s", "10s", "30s", "60s", "120s", "300s", "600s"]
    }
  }

  metrics_flush_interval = "15s"
  aggregation_temporality = "CUMULATIVE"

  output {
    metrics = [otelcol.exporter.prometheus.default.input]
  }
}
```

This generates:
- `roundtable_duration_milliseconds_bucket` — histogram of span duration
- `roundtable_calls_total` — counter of invocations

Both include labels: `service_name`, `span_name`, `span_kind`, `status_code`, plus our custom dimensions (`roundtable_role`, `roundtable_gemini_status`, etc.)

### 2. Alloy Manifest — Update ConfigMap + restart (brahma)

Apply the updated config:
```bash
# On brahma
kubectl create configmap alloy-config --from-file=config.alloy=<updated-config> --dry-run=client -o yaml | kubectl apply -f -
kubectl rollout restart daemonset/alloy
kubectl rollout status daemonset/alloy --timeout=60s
```

### 3. Grafana Dashboard — "Roundtable Skill" (brahma)

Create a new dashboard with these panels:

**CRITICAL: All queries MUST filter by `span_name` because child spans (roundtable.gemini, roundtable.codex) also generate calls/duration metrics. Without filtering, counts are 3x inflated.**

**CRITICAL: spanmetrics uses `STATUS_CODE_OK` / `STATUS_CODE_ERROR` / `STATUS_CODE_UNSET` — not `Ok` or `Error`.**

**Row 1: Overview** (filter to root span only)
- **Invocations** (stat): `sum(increase(roundtable_calls_total{span_name="roundtable.invoke"}[24h]))`
- **Success Rate** (stat): `sum(increase(roundtable_calls_total{span_name="roundtable.invoke", status_code="STATUS_CODE_OK"}[24h])) / sum(increase(roundtable_calls_total{span_name="roundtable.invoke"}[24h])) * 100`
- **Avg Duration** (stat): `sum(rate(roundtable_duration_milliseconds_sum{span_name="roundtable.invoke"}[1h])) / sum(rate(roundtable_duration_milliseconds_count{span_name="roundtable.invoke"}[1h])) / 1000` (in seconds)

**Row 2: Status by CLI** (filter to root span — it carries both CLI statuses as dimensions)
- **Gemini Status** (timeseries, stacked): `sum by (roundtable_gemini_status) (increase(roundtable_calls_total{span_name="roundtable.invoke"}[1h]))`
- **Codex Status** (timeseries, stacked): `sum by (roundtable_codex_status) (increase(roundtable_calls_total{span_name="roundtable.invoke"}[1h]))`

**Row 3: Latency** (filter to child spans — each has its own duration histogram)
- **Gemini p50/p95** (timeseries): `histogram_quantile(0.95, sum by (le) (rate(roundtable_duration_milliseconds_bucket{span_name="roundtable.gemini"}[1h])))`
- **Codex p50/p95** (timeseries): `histogram_quantile(0.95, sum by (le) (rate(roundtable_duration_milliseconds_bucket{span_name="roundtable.codex"}[1h])))`

**Row 4: Role Distribution** (root span)
- **Roles** (pie chart): `sum by (roundtable_role) (increase(roundtable_calls_total{span_name="roundtable.invoke"}[24h]))`

**Row 5: Sessions** (root span)
- **Invocations by Host** (table): `sum by (host_name) (increase(roundtable_calls_total{span_name="roundtable.invoke"}[24h]))`
- **Invocations by Project** (table): `sum by (project_name) (increase(roundtable_calls_total{span_name="roundtable.invoke"}[24h]))`

### 4. Roundtable Telemetry — Emit child spans for per-CLI latency

To get per-CLI histograms (not just the total span duration), change `Roundtable.Telemetry` to emit 3 spans:
1. Root span: `roundtable.invoke` (overall timing, role, files_count, both CLI statuses as dimensions)
2. Child span: `roundtable.gemini` (gemini-specific: status, model) — duration = `elapsed_ms` from runner result
3. Child span: `roundtable.codex` (codex-specific: status, model) — duration = `elapsed_ms` from runner result

**IMPORTANT (from Codex review)**: Since both CLIs run concurrently, child span start/end times must be computed from actual `elapsed_ms` values, NOT from sequential timestamps. Each child span's `endTimeUnixNano` = root span's `endTimeUnixNano`, and `startTimeUnixNano` = `endTimeUnixNano - (elapsed_ms * 1_000_000)`. This correctly represents concurrent execution where both tools overlap in time.

All 3 spans share the same `traceId`. Child spans set `parentSpanId` = root span's `spanId`.

This lets spanmetrics generate separate histograms for `span_name=roundtable.gemini` and `span_name=roundtable.codex` automatically.

### 5. Documentation — Update monitoring.md (homelab-docs)

Add a section to `brahma/monitoring.md` documenting:
- The spanmetrics pipeline
- The Grafana dashboard
- What metrics are generated

## Execution Order

1. Update Alloy config (add spanmetrics connector + traces output)
2. Apply config to brahma, restart Alloy
3. Update Roundtable telemetry to emit child spans
4. Rebuild escript, update release, redeploy
5. Create Grafana dashboard
6. Update monitoring.md
7. Verify end-to-end: run roundtable → check Prometheus metrics → check Grafana panels

## Notes

- The existing `otelcol.exporter.prometheus "default"` has `resource_to_telemetry_conversion = true` — this automatically promotes `service_name`, `project_name`, `host_name`, `session_tag` from resource attributes to Prometheus metric labels. No extra config needed for the session/host dashboard panels.
- Dots in span attribute names become underscores in Prometheus labels: `roundtable.gemini.status` → `roundtable_gemini_status`

## Risks

- spanmetrics generates high-cardinality metrics if too many dimensions are included (mitigated by limiting to 5 custom dimensions, ~3125 max series)
- OTLP HTTP receiver expects protobuf by default; roundtable sends JSON — Alloy auto-detects content-type, confirmed working
- No Tempo means individual traces can't be browsed — only aggregated metrics. Acceptable for v1.

## Out of Scope

- Tempo deployment (trace browsing)
- Alert rules on roundtable metrics
- Retention policy changes
