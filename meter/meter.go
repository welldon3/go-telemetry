// Package meter wraps the OpenTelemetry meter with a simpler API
// for the most common metric types.
package meter

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
)

type Meter interface {
	Counter(name, description string) (Counter, error)
	// UpDownCounter is for values that go up and down (active connections, queue depth).
	UpDownCounter(name, description string) (UpDownCounter, error)
	Histogram(name, description, unit string, opts ...HistogramOption) (Histogram, error)
	Gauge(name, description string, fn func() float64) error
}

type Counter interface {
	Add(ctx context.Context, value int64, attrs ...attribute.KeyValue)
	Inc(ctx context.Context, attrs ...attribute.KeyValue)
}

type UpDownCounter interface {
	Add(ctx context.Context, value int64, attrs ...attribute.KeyValue)
}

type Histogram interface {
	Record(ctx context.Context, value float64, attrs ...attribute.KeyValue)
}

// HistogramOption configures a Histogram at creation time.
type HistogramOption func(*histogramCfg)

type histogramCfg struct {
	explicitBounds []float64
}

// WithExplicitBuckets sets explicit bucket boundaries.
// Example: meter.WithExplicitBuckets([]float64{5, 10, 25, 50, 100, 250, 500, 1000})
func WithExplicitBuckets(bounds []float64) HistogramOption {
	return func(c *histogramCfg) { c.explicitBounds = bounds }
}

type otelMeter struct {
	inner otelmetric.Meter
}

// New wraps an OTel meter.
func New(m otelmetric.Meter) Meter {
	return &otelMeter{inner: m}
}

func (m *otelMeter) UpDownCounter(name, description string) (UpDownCounter, error) {
	c, err := m.inner.Int64UpDownCounter(name,
		otelmetric.WithDescription(description),
	)
	if err != nil {
		return nil, fmt.Errorf("meter: create up-down counter %q: %w", name, err)
	}
	return &otelUpDownCounter{inner: c}, nil
}

func (m *otelMeter) Counter(name, description string) (Counter, error) {
	c, err := m.inner.Int64Counter(name,
		otelmetric.WithDescription(description),
	)
	if err != nil {
		return nil, fmt.Errorf("meter: create counter %q: %w", name, err)
	}
	return &otelCounter{inner: c}, nil
}

func (m *otelMeter) Histogram(name, description, unit string, opts ...HistogramOption) (Histogram, error) {
	cfg := &histogramCfg{}
	for _, o := range opts {
		o(cfg)
	}

	var h otelmetric.Float64Histogram
	var err error
	if len(cfg.explicitBounds) > 0 {
		h, err = m.inner.Float64Histogram(name,
			otelmetric.WithDescription(description),
			otelmetric.WithUnit(unit),
			otelmetric.WithExplicitBucketBoundaries(cfg.explicitBounds...),
		)
	} else {
		h, err = m.inner.Float64Histogram(name,
			otelmetric.WithDescription(description),
			otelmetric.WithUnit(unit),
		)
	}
	if err != nil {
		return nil, fmt.Errorf("meter: create histogram %q: %w", name, err)
	}
	return &otelHistogram{inner: h}, nil
}

func (m *otelMeter) Gauge(name, description string, fn func() float64) error {
	_, err := m.inner.Float64ObservableGauge(name,
		otelmetric.WithDescription(description),
		otelmetric.WithFloat64Callback(func(_ context.Context, o otelmetric.Float64Observer) error {
			o.Observe(fn())
			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("meter: create gauge %q: %w", name, err)
	}
	return nil
}

type otelUpDownCounter struct {
	inner otelmetric.Int64UpDownCounter
}

func (c *otelUpDownCounter) Add(ctx context.Context, value int64, attrs ...attribute.KeyValue) {
	c.inner.Add(ctx, value, otelmetric.WithAttributes(attrs...))
}

type otelCounter struct {
	inner otelmetric.Int64Counter
}

func (c *otelCounter) Add(ctx context.Context, value int64, attrs ...attribute.KeyValue) {
	c.inner.Add(ctx, value, otelmetric.WithAttributes(attrs...))
}

func (c *otelCounter) Inc(ctx context.Context, attrs ...attribute.KeyValue) {
	c.inner.Add(ctx, 1, otelmetric.WithAttributes(attrs...))
}

type otelHistogram struct {
	inner otelmetric.Float64Histogram
}

func (h *otelHistogram) Record(ctx context.Context, value float64, attrs ...attribute.KeyValue) {
	h.inner.Record(ctx, value, otelmetric.WithAttributes(attrs...))
}
