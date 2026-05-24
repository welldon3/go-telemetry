// Package telemetry provides a unified observability API that automatically
// correlates logs, traces, and metrics through a shared context.Context.
// trace_id and span_id are injected into log entries without any extra wiring.
//
// Usage:
//
//	tel, err := telemetry.New(ctx, telemetry.Config{
//	    ServiceName: "my-service",
//	    Exporter:    telemetry.ExporterOTLP,
//	    OTLPEndpoint: "localhost:4317",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer tel.Shutdown(ctx)
package telemetry

import (
	"context"
	"fmt"
	"os"

	"github.com/welldon3/go-telemetry/internal/export"
	"github.com/welldon3/go-telemetry/logger"
	"github.com/welldon3/go-telemetry/meter"
	"github.com/welldon3/go-telemetry/tracer"
	"go.opentelemetry.io/otel"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

const (
	defaultPrometheusPort = 9090
	defaultSamplingRatio  = 1.0
)

// ExporterType selects the backend where telemetry data is sent.
type ExporterType string

const (
	ExporterStdout     ExporterType = "stdout"
	ExporterOTLP       ExporterType = "otlp"
	ExporterPrometheus ExporterType = "prometheus"
	ExporterNoop       ExporterType = "noop"
)

// Config holds all options for creating a Provider.
type Config struct {
	ServiceName    string // required
	ServiceVersion string
	Environment    string

	// ResourceAttributes are extra key-value pairs attached to the OTel resource.
	ResourceAttributes map[string]string

	// Exporter is the primary trace backend. Set Exporters for fan-out.
	Exporter  ExporterType
	Exporters []ExporterType // overrides Exporter when non-empty

	OTLPEndpoint string // gRPC endpoint, e.g. "localhost:4317"
	OTLPInsecure bool   // skip TLS; local dev only

	PrometheusPort int // defaults to 9090

	// SamplingRatio is the fraction of traces to sample (0.0–1.0).
	SamplingRatio float64
	// AlwaysSampleErrors forces sampling for any trace that records an error,
	// regardless of SamplingRatio.
	AlwaysSampleErrors bool

	LogLevel    logger.Level
	LogExporter logger.LogExporter // optional; ships logs to a remote backend
}

// Provider is the central handle for all telemetry signals.
type Provider struct {
	cfg         Config
	logger      logger.Logger
	tracer      tracer.Tracer
	meter       meter.Meter
	shutdownFns []func(context.Context) error
}

// New initialises a Provider and registers global OTel tracer and meter
// providers, so instrumented third-party libraries share the same pipeline.
func New(ctx context.Context, cfg Config) (*Provider, error) {
	if cfg.ServiceName == "" {
		return nil, fmt.Errorf("telemetry: ServiceName is required")
	}
	if cfg.SamplingRatio == 0 {
		cfg.SamplingRatio = defaultSamplingRatio
	}
	if cfg.PrometheusPort == 0 {
		cfg.PrometheusPort = defaultPrometheusPort
	}

	resAttrs := []attribute.KeyValue{
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.ServiceVersion),
		semconv.DeploymentEnvironment(cfg.Environment),
	}
	for k, v := range cfg.ResourceAttributes {
		resAttrs = append(resAttrs, attribute.String(k, v))
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(resAttrs...),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: build resource: %w", err)
	}

	p := &Provider{cfg: cfg}

	// --- Tracer provider --------------------------------------------------
	exporterTypes := make([]string, 0, max(len(cfg.Exporters), 1))
	if len(cfg.Exporters) > 0 {
		for _, e := range cfg.Exporters {
			exporterTypes = append(exporterTypes, string(e))
		}
	} else {
		exporterTypes = append(exporterTypes, string(cfg.Exporter))
	}

	tp, shutdownTrace, err := export.BuildTracerProvider(ctx, exporterTypes, cfg.OTLPEndpoint, cfg.OTLPInsecure, res,
		export.SamplerFromConfig(cfg.SamplingRatio, cfg.AlwaysSampleErrors),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: tracer provider: %w", err)
	}
	otel.SetTracerProvider(tp)
	p.shutdownFns = append(p.shutdownFns, shutdownTrace)
	p.tracer = tracer.New(tp.Tracer(cfg.ServiceName))

	// --- Meter provider ---------------------------------------------------
	mp, shutdownMetrics, err := export.BuildMeterProvider(ctx, string(cfg.Exporter), cfg.OTLPEndpoint, cfg.OTLPInsecure, cfg.PrometheusPort, res)
	if err != nil {
		_ = p.Shutdown(ctx) // clean up tracer registered above
		return nil, fmt.Errorf("telemetry: meter provider: %w", err)
	}
	otel.SetMeterProvider(mp)
	p.shutdownFns = append(p.shutdownFns, shutdownMetrics)
	p.meter = meter.New(mp.Meter(cfg.ServiceName))

	// --- Logger -----------------------------------------------------------
	logCfg := logger.Config{
		Level:          cfg.LogLevel,
		ServiceName:    cfg.ServiceName,
		ServiceVersion: cfg.ServiceVersion,
	}
	if cfg.LogExporter != nil {
		var shutdownLog func(context.Context) error
		p.logger, shutdownLog = logger.NewWithExporter(logCfg, os.Stdout, cfg.LogExporter)
		p.shutdownFns = append(p.shutdownFns, shutdownLog)
	} else {
		p.logger = logger.New(logCfg)
	}

	return p, nil
}

// Logger returns the structured logger.
func (p *Provider) Logger() logger.Logger { return p.logger }

func (p *Provider) Tracer() tracer.Tracer { return p.tracer }
func (p *Provider) Meter() meter.Meter    { return p.meter }

// OTelTracer returns the raw OTel tracer, for libraries that need it directly.
func (p *Provider) OTelTracer() oteltrace.Tracer {
	return otel.GetTracerProvider().Tracer(p.cfg.ServiceName)
}

// OTelMeter returns the raw OTel meter, for libraries that need it directly.
func (p *Provider) OTelMeter() otelmetric.Meter {
	return otel.GetMeterProvider().Meter(p.cfg.ServiceName)
}

// Shutdown flushes all pending telemetry and releases resources.
func (p *Provider) Shutdown(ctx context.Context) error {
	var errs []error
	for i := len(p.shutdownFns) - 1; i >= 0; i-- {
		if err := p.shutdownFns[i](ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("telemetry shutdown errors: %v", errs)
	}
	return nil
}

// ForceFlush exports all buffered telemetry synchronously.
// Call before Shutdown in CLIs and batch jobs so spans are not dropped.
func (p *Provider) ForceFlush(ctx context.Context) error {
	var errs []error
	if tp, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider); ok {
		if err := tp.ForceFlush(ctx); err != nil {
			errs = append(errs, fmt.Errorf("traces: %w", err))
		}
	}
	if mp, ok := otel.GetMeterProvider().(*metric.MeterProvider); ok {
		if err := mp.ForceFlush(ctx); err != nil {
			errs = append(errs, fmt.Errorf("metrics: %w", err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("telemetry force flush: %v", errs)
	}
	return nil
}
