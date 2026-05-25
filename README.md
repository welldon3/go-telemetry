# go-telemetry

[![CI](https://github.com/welldon3/go-telemetry/actions/workflows/ci.yml/badge.svg)](https://github.com/welldon3/go-telemetry/actions/workflows/ci.yml)
[![Go 1.22+](https://img.shields.io/badge/go-1.22+-00ADD8.svg?logo=go)](https://go.dev/)
[![Go Report Card](https://goreportcard.com/badge/github.com/welldon3/go-telemetry)](https://goreportcard.com/report/github.com/welldon3/go-telemetry)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Structured logging, distributed tracing, and metrics for Go, correlated automatically through `context`.

## Why

Most Go services wire up `slog`, `opentelemetry-go`, and `prometheus/client_golang` as three separate pipelines with no correlation. You see a slow request in Jaeger but cannot find the matching log line because `trace_id` never made it there.

`go-telemetry` fixes this with a single `Provider` that injects `trace_id` and `span_id` into every log entry automatically, exposes a thin API over the OTel SDK, and ships pluggable exporters for dev and production.

## Install

```sh
go get github.com/welldon3/go-telemetry
```

## Quick start

```go
tel, err := telemetry.New(ctx, telemetry.Config{
    ServiceName:        "my-service",
    ServiceVersion:     "1.2.0",
    Environment:        "production",
    Exporter:           telemetry.ExporterOTLP,
    OTLPEndpoint:       "localhost:4317",
    SamplingRatio:      0.1,
    AlwaysSampleErrors: true,
})
if err != nil {
    log.Fatal(err)
}
defer tel.Shutdown(ctx)

log    := tel.Logger()
tracer := tel.Tracer()
meter  := tel.Meter()
```

## Local observability stack

The repo ships a Docker Compose stack that wires Grafana, Loki, Tempo, and Prometheus so you can see traces, logs, and metrics live in under a minute.

**Requirements:** Docker with Compose v2.

```sh
# 1. Start the stack (Grafana :3000, Tempo :4317, Loki :3100, Prometheus :9090)
docker compose up -d

# 2. Run the instrumented example server
make example          # or: go run ./examples/basic

# 3. Send some traffic
curl "http://localhost:8080/orders?id=42"
curl "http://localhost:8080/orders?id=99"
curl "http://localhost:8080/orders"          # triggers a validation error
```

Open **http://localhost:3000** (no login required).

| Signal | Where to look |
|---|---|
| Traces | Explore → Tempo → Search, or paste a `traceID` |
| Logs | Explore → Loki → `{service_name="example-service"}` |
| Metrics | Explore → Prometheus → `orders_processed_total` |

**Log → Trace correlation:** In the Loki log line, click the `trace_id` value — Grafana jumps straight to the matching trace in Tempo.

```sh
# Stop the stack when you're done
docker compose down
```

## Logging

```go
// trace_id and span_id are injected automatically when ctx carries an active span.
log.Info(ctx, "user signed in", "user_id", userID)
log.Error(ctx, "db query failed", "error", err, "query", sql)

// Child logger with fixed fields.
reqLog := log.With("request_id", r.Header.Get("X-Request-ID"))
```

## Tracing

```go
ctx, span := tracer.Start(ctx, "db.query",
    tracer.WithDB("postgres", "SELECT * FROM orders WHERE id = $1"),
)
defer span.End()

if err := db.QueryRowContext(ctx, ...).Scan(&order); err != nil {
    span.SetError(err)
    return err
}
span.SetAttribute("order.id", order.ID)
```

Link a span to another trace (async handoffs, queue producers/consumers):

```go
ctx, span := tracer.Start(ctx, "process.job",
    tracer.WithLink(publishCtx), // links to the publisher's span
)
```

## Metrics

```go
// Counter
reqs, _ := meter.Counter("http.requests.total", "Total HTTP requests")
reqs.Add(ctx, 1, attribute.String("method", "GET"))

// UpDownCounter (values that go up and down)
conns, _ := meter.UpDownCounter("db.connections.active", "Active DB connections")
conns.Add(ctx, 1)
// ... later:
conns.Add(ctx, -1)

// Histogram with explicit buckets
latency, _ := meter.Histogram("db.query.duration", "Query latency", "ms",
    meter.WithExplicitBuckets([]float64{1, 5, 10, 25, 50, 100, 250, 500}),
)
latency.Record(ctx, elapsed.Milliseconds())

// Gauge (polled on collection)
meter.Gauge("queue.depth", "Pending jobs", func() float64 {
    return float64(queue.Len())
})
```

## HTTP middleware

### Server

```go
instrumented := middleware.HTTP(middleware.HTTPConfig{
    Tracer: tel.Tracer(),
    Logger: tel.Logger(),
    Meter:  tel.Meter(),
    // Go 1.22+ route patterns:
    RouteFunc: func(r *http.Request) string { return r.Pattern },
})(mux)

http.ListenAndServe(":8080", instrumented)
```

Extracts `traceparent`/`tracestate` from incoming headers, creates a server span, records `http.server.request.duration` and `http.server.request.total`, marks 5xx as errors, logs each request with `trace_id`.

### Client

```go
client := middleware.NewHTTPClient(middleware.HTTPClientConfig{
    Tracer: tel.Tracer(),
    Meter:  tel.Meter(),
})

resp, err := client.Get("https://api.example.com/orders")
```

Propagates trace context to outgoing requests, creates a client span, records `http.client.request.duration`, marks 5xx as errors.

## gRPC middleware

### Server

```go
srv := grpc.NewServer(
    grpc.UnaryInterceptor(middleware.UnaryServerInterceptor(middleware.GRPCConfig{
        Tracer: tel.Tracer(),
        Meter:  tel.Meter(),
    })),
    grpc.StreamInterceptor(middleware.StreamServerInterceptor(middleware.GRPCConfig{
        Tracer: tel.Tracer(),
        Meter:  tel.Meter(),
    })),
)
```

### Client

```go
conn, err := grpc.NewClient("localhost:50051",
    grpc.WithUnaryInterceptor(middleware.UnaryClientInterceptor(middleware.GRPCClientConfig{
        Tracer: tel.Tracer(),
        Meter:  tel.Meter(),
    })),
    grpc.WithStreamInterceptor(middleware.StreamClientInterceptor(middleware.GRPCClientConfig{
        Tracer: tel.Tracer(),
    })),
)
```

Both server and client interceptors extract/inject trace context via gRPC metadata, create spans, record duration and total call metrics, and mark non-OK statuses as errors.

## Baggage

W3C Baggage lets you propagate key-value pairs across service boundaries alongside the trace context.

```go
// Attach baggage to the context (propagated through all downstream calls).
ctx = propagation.SetBaggage(ctx, "tenant", "acme")

// Read it anywhere downstream.
tenant := propagation.GetBaggage(ctx, "tenant")

// Get all baggage as a map.
all := propagation.BaggageMap(ctx)
```

Manual header injection/extraction (useful in custom transports):

```go
propagation.InjectHeaders(ctx, req.Header)   // writes traceparent + baggage
ctx = propagation.ExtractHeaders(ctx, r.Header)
```

## Runtime metrics

```go
meter.RegisterRuntimeMetrics(tel.Meter())
```

Registers `go.goroutines`, `go.heap_alloc_bytes`, `go.gc_pause_ns`, `go.heap_objects`.

## Exporters

| Value | Description |
|---|---|
| `telemetry.ExporterStdout` | Pretty-printed JSON to stdout. Default. |
| `telemetry.ExporterOTLP` | gRPC to any OTLP receiver (Jaeger, Tempo, Honeycomb). |
| `telemetry.ExporterPrometheus` | Exposes `/metrics` on `Config.PrometheusPort` (default 9090). |
| `telemetry.ExporterNoop` | Discards everything. Useful in tests. |

Fan-out to multiple backends simultaneously:

```go
telemetry.Config{
    Exporters: []telemetry.ExporterType{
        telemetry.ExporterOTLP,
        telemetry.ExporterStdout,
    },
}
```

## Sampling

```go
telemetry.Config{
    SamplingRatio:      0.05, // sample 5%
    AlwaysSampleErrors: true, // always capture errors
}
```

`AlwaysSampleErrors` wraps the base sampler so any span that records an error is force-sampled regardless of the ratio. Recommended for production.

## Environment variables

`telemetry.ConfigFromEnv()` reads standard OTel environment variables:

| Variable | Description |
|---|---|
| `OTEL_SERVICE_NAME` | Service name |
| `OTEL_TRACES_EXPORTER` | Comma-separated exporter list (`otlp,stdout`) |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP endpoint |
| `OTEL_TRACES_SAMPLER` | `always_on`, `always_off`, `traceidratio` |
| `OTEL_TRACES_SAMPLER_ARG` | Sampling ratio for `traceidratio` |
| `OTEL_RESOURCE_ATTRIBUTES` | Comma-separated `key=value` resource attributes |

## CLIs and batch jobs

Call `ForceFlush` before `Shutdown` so in-flight spans are not dropped when the process exits:

```go
defer func() {
    tel.ForceFlush(ctx)
    tel.Shutdown(ctx)
}()
```

## Log-to-trace correlation

Every log entry carries the active span's identifiers:

```json
{
  "time": "2024-03-01T12:00:00Z",
  "level": "INFO",
  "msg": "order processed",
  "service.name": "my-service",
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
  "span_id": "00f067aa0ba902b7",
  "trace_flags": "01",
  "order_id": "ord_123"
}
```

In Grafana, add a derived field on `trace_id` pointing at Tempo and clicking a log line opens the matching trace directly.

## Testing

```go
import "github.com/welldon3/go-telemetry/telemetrytest"

rec, tr := telemetrytest.NewSpanRecorder()
// pass tr to code under test

span, ok := rec.SpanNamed("processOrder")
if !ok {
    t.Fatal("span not found")
}
// assert on span.Attributes, span.Status, etc.

// NoopLogger satisfies logger.Logger without producing output.
svc := NewService(tr, telemetrytest.NoopLogger())
```

## Project structure

```
go-telemetry/
├── telemetry.go          # Provider, New(), Shutdown(), ForceFlush()
├── env.go                # ConfigFromEnv()
├── logger/               # Logger interface + slog implementation
├── tracer/               # Tracer interface, OTel wrapper, ErrorSampler
├── meter/                # Meter interface, OTel wrapper, runtime metrics
├── propagation/          # InjectHeaders, ExtractHeaders, Baggage helpers
├── middleware/
│   ├── http.go           # HTTP server middleware
│   ├── http_client.go    # HTTP client transport
│   ├── grpc.go           # gRPC server interceptors
│   └── grpc_client.go    # gRPC client interceptors
├── telemetrytest/        # SpanRecorder, NoopLogger
├── internal/export/      # Backend wiring (stdout, OTLP, Prometheus)
└── examples/
    ├── cli/              # Batch job with ForceFlush
    └── grpc/             # gRPC server + client with trace propagation
```

## Contributing

PRs welcome. Please add tests for any new exporter or middleware.

## License

MIT
