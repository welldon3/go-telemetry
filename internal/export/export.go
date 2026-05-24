// Package export wires OpenTelemetry SDK providers to different backends.
// It is an internal package : use the telemetry.Config.Exporter field
// to select a backend rather than calling these functions directly.
package export

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/welldon3/go-telemetry/tracer"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	prometheusexporter "go.opentelemetry.io/otel/exporters/prometheus"
	stdoutmetric "go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	stdouttrace "go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// ExporterType mirrors telemetry.ExporterType to avoid an import cycle.
type ExporterType = string

// SamplerFromConfig builds an OTel sampler from the provider config values.
func SamplerFromConfig(ratio float64, alwaysSampleErrors bool) sdktrace.Sampler {
	var base sdktrace.Sampler
	if ratio >= 1.0 {
		base = sdktrace.AlwaysSample()
	} else if ratio <= 0 {
		base = sdktrace.NeverSample()
	} else {
		base = sdktrace.TraceIDRatioBased(ratio)
	}

	if alwaysSampleErrors {
		return tracer.NewErrorSampler(base)
	}
	return base
}

// BuildTracerProvider creates an SDK TracerProvider for the requested backends.
// Pass multiple exporterTypes to fan-out spans to several backends simultaneously.
// Set otlpInsecure=true only for local development; production should use TLS.
func BuildTracerProvider(
	ctx context.Context,
	exporterTypes []ExporterType,
	otlpEndpoint string,
	otlpInsecure bool,
	res *resource.Resource,
	sampler sdktrace.Sampler,
) (*sdktrace.TracerProvider, func(context.Context) error, error) {

	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	}

	hasActiveExporter := false
	for _, et := range exporterTypes {
		if et == "noop" {
			continue
		}
		exp, err := buildSpanExporter(ctx, et, otlpEndpoint, otlpInsecure)
		if err != nil {
			return nil, noop, err
		}
		opts = append(opts, sdktrace.WithBatcher(exp))
		hasActiveExporter = true
	}

	if !hasActiveExporter {
		// All exporters were noop (or list was empty).
		tp := sdktrace.NewTracerProvider()
		return tp, tp.Shutdown, nil
	}

	tp := sdktrace.NewTracerProvider(opts...)
	return tp, tp.Shutdown, nil
}

func buildSpanExporter(ctx context.Context, exporterType, otlpEndpoint string, otlpInsecure bool) (sdktrace.SpanExporter, error) {
	switch exporterType {
	case "otlp":
		if otlpEndpoint == "" {
			return nil, fmt.Errorf("export: OTLPEndpoint is required for OTLP exporter")
		}
		creds := credentials.NewClientTLSFromCert(nil, "")
		if otlpInsecure {
			creds = insecure.NewCredentials()
		}
		conn, err := grpc.NewClient(otlpEndpoint, grpc.WithTransportCredentials(creds))
		if err != nil {
			return nil, fmt.Errorf("export: dial OTLP endpoint %q: %w", otlpEndpoint, err)
		}
		exp, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
		if err != nil {
			return nil, fmt.Errorf("export: OTLP trace exporter: %w", err)
		}
		return exp, nil
	default: // "stdout" and anything unrecognised
		return stdouttrace.New(stdouttrace.WithPrettyPrint())
	}
}

// BuildMeterProvider creates an SDK MeterProvider for the requested backend.
// Set otlpInsecure=true only for local development; production should use TLS.
func BuildMeterProvider(
	ctx context.Context,
	exporterType ExporterType,
	otlpEndpoint string,
	otlpInsecure bool,
	prometheusPort int,
	res *resource.Resource,
) (otelmetric.MeterProvider, func(context.Context) error, error) {

	switch exporterType {
	case "prometheus":
		return buildPrometheusProvider(res, prometheusPort)

	case "otlp":
		return buildOTLPMetricProvider(ctx, otlpEndpoint, otlpInsecure, res)

	case "noop":
		mp := metric.NewMeterProvider()
		return mp, mp.Shutdown, nil

	default: // stdout
		return buildStdoutMetricProvider(res)
	}
}

func buildStdoutMetricProvider(res *resource.Resource) (otelmetric.MeterProvider, func(context.Context) error, error) {
	exp, err := stdoutmetric.New()
	if err != nil {
		return nil, noop, fmt.Errorf("export: stdout metric exporter: %w", err)
	}
	mp := metric.NewMeterProvider(
		metric.WithReader(metric.NewPeriodicReader(exp)),
		metric.WithResource(res),
	)
	return mp, mp.Shutdown, nil
}

func buildPrometheusProvider(res *resource.Resource, port int) (otelmetric.MeterProvider, func(context.Context) error, error) {
	exp, err := prometheusexporter.New()
	if err != nil {
		return nil, noop, fmt.Errorf("export: prometheus exporter: %w", err)
	}
	mp := metric.NewMeterProvider(
		metric.WithReader(exp),
		metric.WithResource(res),
	)

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, noop, fmt.Errorf("export: prometheus: listen on port %d: %w", port, err)
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()

	shutdown := func(ctx context.Context) error {
		_ = srv.Shutdown(ctx)
		return mp.Shutdown(ctx)
	}
	return mp, shutdown, nil
}

func buildOTLPMetricProvider(ctx context.Context, endpoint string, otlpInsecure bool, res *resource.Resource) (otelmetric.MeterProvider, func(context.Context) error, error) {
	if endpoint == "" {
		return nil, noop, fmt.Errorf("export: OTLPEndpoint is required for OTLP exporter")
	}
	creds := credentials.NewClientTLSFromCert(nil, "")
	if otlpInsecure {
		creds = insecure.NewCredentials()
	}
	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, noop, fmt.Errorf("export: dial OTLP metric endpoint %q: %w", endpoint, err)
	}
	exp, err := otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, noop, fmt.Errorf("export: OTLP metric exporter: %w", err)
	}
	mp := metric.NewMeterProvider(
		metric.WithReader(metric.NewPeriodicReader(exp)),
		metric.WithResource(res),
	)
	return mp, mp.Shutdown, nil
}

func noop(_ context.Context) error { return nil }
