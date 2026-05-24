package telemetry

import (
	"context"
	"os"
	"testing"
)

func TestNew_RequiresServiceName(t *testing.T) {
	_, err := New(context.Background(), Config{})
	if err == nil {
		t.Fatal("expected error for empty ServiceName")
	}
}

func TestNew_Noop(t *testing.T) {
	tel, err := New(context.Background(), Config{
		ServiceName: "test-svc",
		Exporter:    ExporterNoop,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if tel.Logger() == nil {
		t.Error("Logger() should not be nil")
	}
	if tel.Tracer() == nil {
		t.Error("Tracer() should not be nil")
	}
	if tel.Meter() == nil {
		t.Error("Meter() should not be nil")
	}
}

func TestNew_DefaultsSamplingRatio(t *testing.T) {
	tel, err := New(context.Background(), Config{
		ServiceName:   "test-svc",
		Exporter:      ExporterNoop,
		SamplingRatio: 0, // should default to 1.0
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if tel.cfg.SamplingRatio != 1.0 {
		t.Errorf("SamplingRatio: want 1.0, got %v", tel.cfg.SamplingRatio)
	}
}

func TestProvider_Shutdown(t *testing.T) {
	tel, err := New(context.Background(), Config{
		ServiceName: "test-svc",
		Exporter:    ExporterNoop,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown() error: %v", err)
	}
}

func TestProvider_ForceFlush(t *testing.T) {
	tel, err := New(context.Background(), Config{
		ServiceName: "test-svc",
		Exporter:    ExporterNoop,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if err := tel.ForceFlush(context.Background()); err != nil {
		t.Errorf("ForceFlush() error: %v", err)
	}
}

func TestProvider_OTelAccessors(t *testing.T) {
	tel, err := New(context.Background(), Config{
		ServiceName: "test-svc",
		Exporter:    ExporterNoop,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if tel.OTelTracer() == nil {
		t.Error("OTelTracer() should not be nil")
	}
	if tel.OTelMeter() == nil {
		t.Error("OTelMeter() should not be nil")
	}
}

func TestConfigFromEnv_ExporterType(t *testing.T) {
	t.Setenv("OTEL_TRACES_EXPORTER", "otlp")
	cfg := ConfigFromEnv(Config{})
	if cfg.Exporter != ExporterOTLP {
		t.Errorf("OTEL_TRACES_EXPORTER=otlp: want ExporterOTLP, got %q", cfg.Exporter)
	}

	t.Setenv("OTEL_TRACES_EXPORTER", "none")
	cfg = ConfigFromEnv(Config{})
	if cfg.Exporter != ExporterNoop {
		t.Errorf("OTEL_TRACES_EXPORTER=none: want ExporterNoop, got %q", cfg.Exporter)
	}
}

func TestConfigFromEnv_ResourceAttributes(t *testing.T) {
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "team=platform,region=us-east-1")
	cfg := ConfigFromEnv(Config{})
	if cfg.ResourceAttributes["team"] != "platform" {
		t.Errorf("team: want %q, got %q", "platform", cfg.ResourceAttributes["team"])
	}
	if cfg.ResourceAttributes["region"] != "us-east-1" {
		t.Errorf("region: want %q, got %q", "us-east-1", cfg.ResourceAttributes["region"])
	}
}

func TestConfigFromEnv_Sampler(t *testing.T) {
	os.Unsetenv("OTEL_TRACES_SAMPLER_ARG")

	t.Setenv("OTEL_TRACES_SAMPLER", "always_off")
	cfg := ConfigFromEnv(Config{SamplingRatio: 1.0})
	if cfg.SamplingRatio != 0.0 {
		t.Errorf("always_off: want 0.0, got %v", cfg.SamplingRatio)
	}

	t.Setenv("OTEL_TRACES_SAMPLER", "traceidratio")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.25")
	cfg = ConfigFromEnv(Config{})
	if cfg.SamplingRatio != 0.25 {
		t.Errorf("traceidratio: want 0.25, got %v", cfg.SamplingRatio)
	}
}

func TestNew_Stdout(t *testing.T) {
	tel, err := New(context.Background(), Config{
		ServiceName: "test-svc",
		Exporter:    ExporterStdout,
	})
	if err != nil {
		t.Fatalf("New() with stdout error: %v", err)
	}
	defer tel.Shutdown(context.Background())
}