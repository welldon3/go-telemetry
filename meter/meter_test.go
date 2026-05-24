package meter

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// noopMeter returns a Meter backed by a provider with no exporters,
// so all measurements are recorded but discarded — safe for unit tests.
func noopMeter() Meter {
	mp := sdkmetric.NewMeterProvider()
	return New(mp.Meter("test"))
}

func TestCounter_AddAndInc(t *testing.T) {
	m := noopMeter()
	c, err := m.Counter("test.counter", "A test counter")
	if err != nil {
		t.Fatalf("Counter() error: %v", err)
	}

	// Must not panic.
	c.Add(context.Background(), 5)
	c.Inc(context.Background())
}

func TestHistogram_Record(t *testing.T) {
	m := noopMeter()
	h, err := m.Histogram("test.histogram", "A test histogram", "ms")
	if err != nil {
		t.Fatalf("Histogram() error: %v", err)
	}

	// Must not panic.
	h.Record(context.Background(), 42.5)
}

func TestGauge_Register(t *testing.T) {
	m := noopMeter()
	err := m.Gauge("test.gauge", "A test gauge", func() float64 { return 1.0 })
	if err != nil {
		t.Errorf("Gauge() error: %v", err)
	}
}

func TestRuntimeMetrics(t *testing.T) {
	m := noopMeter()
	if err := RegisterRuntimeMetrics(m); err != nil {
		t.Errorf("RegisterRuntimeMetrics() error: %v", err)
	}
}

func TestUpDownCounter_AddPositiveAndNegative(t *testing.T) {
	m := noopMeter()
	c, err := m.UpDownCounter("test.updown", "An up-down counter")
	if err != nil {
		t.Fatalf("UpDownCounter() error: %v", err)
	}

	// Must not panic for positive and negative deltas.
	c.Add(context.Background(), 5)
	c.Add(context.Background(), -3)
	c.Add(context.Background(), 0)
}

func TestCounter_DuplicateName(t *testing.T) {
	m := noopMeter()
	_, err := m.Counter("dup.counter", "first")
	if err != nil {
		t.Fatalf("first Counter() error: %v", err)
	}
	// OTel SDK allows re-registering the same instrument — must not error.
	_, err = m.Counter("dup.counter", "second")
	if err != nil {
		t.Errorf("re-registering counter should not error: %v", err)
	}
}
