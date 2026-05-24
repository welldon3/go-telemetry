package export

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestSamplerFromConfig_AlwaysSample(t *testing.T) {
	s := SamplerFromConfig(1.0, false)
	if s.Description() != sdktrace.AlwaysSample().Description() {
		t.Errorf("ratio=1.0 should give AlwaysSample, got %q", s.Description())
	}
}

func TestSamplerFromConfig_NeverSample(t *testing.T) {
	s := SamplerFromConfig(0, false)
	if s.Description() != sdktrace.NeverSample().Description() {
		t.Errorf("ratio=0 should give NeverSample, got %q", s.Description())
	}
}

func TestSamplerFromConfig_RatioBased(t *testing.T) {
	s := SamplerFromConfig(0.5, false)
	want := sdktrace.TraceIDRatioBased(0.5).Description()
	if s.Description() != want {
		t.Errorf("ratio=0.5 description: want %q, got %q", want, s.Description())
	}
}

func TestSamplerFromConfig_WithErrorSampler(t *testing.T) {
	s := SamplerFromConfig(0.1, true)
	// ErrorSampler wraps the base sampler — description should mention it.
	if s.Description() == sdktrace.TraceIDRatioBased(0.1).Description() {
		t.Error("alwaysSampleErrors=true should wrap with ErrorSampler")
	}
}

func TestBuildTracerProvider_Noop(t *testing.T) {
	res := resource.Default()
	tp, shutdown, err := BuildTracerProvider(
		context.Background(), []string{"noop"}, "", false, res, sdktrace.AlwaysSample(),
	)
	if err != nil {
		t.Fatalf("BuildTracerProvider(noop) error: %v", err)
	}
	if tp == nil {
		t.Fatal("expected non-nil TracerProvider")
	}
	defer shutdown(context.Background())
}

func TestBuildTracerProvider_Stdout(t *testing.T) {
	res := resource.Default()
	tp, shutdown, err := BuildTracerProvider(
		context.Background(), []string{"stdout"}, "", false, res, sdktrace.AlwaysSample(),
	)
	if err != nil {
		t.Fatalf("BuildTracerProvider(stdout) error: %v", err)
	}
	if tp == nil {
		t.Fatal("expected non-nil TracerProvider")
	}
	defer shutdown(context.Background())
}

func TestBuildTracerProvider_OTLP_EmptyEndpoint(t *testing.T) {
	res := resource.Default()
	_, _, err := BuildTracerProvider(
		context.Background(), []string{"otlp"}, "", false, res, sdktrace.AlwaysSample(),
	)
	if err == nil {
		t.Fatal("expected error for empty OTLP endpoint")
	}
}

func TestBuildTracerProvider_MultiExporter(t *testing.T) {
	res := resource.Default()
	// stdout + stdout: two batchers registered — provider should be valid.
	tp, shutdown, err := BuildTracerProvider(
		context.Background(), []string{"stdout", "stdout"}, "", false, res, sdktrace.AlwaysSample(),
	)
	if err != nil {
		t.Fatalf("BuildTracerProvider(multi) error: %v", err)
	}
	if tp == nil {
		t.Fatal("expected non-nil TracerProvider")
	}
	defer shutdown(context.Background())
}

func TestBuildMeterProvider_Noop(t *testing.T) {
	res := resource.Default()
	mp, shutdown, err := BuildMeterProvider(
		context.Background(), "noop", "", false, 0, res,
	)
	if err != nil {
		t.Fatalf("BuildMeterProvider(noop) error: %v", err)
	}
	if mp == nil {
		t.Fatal("expected non-nil MeterProvider")
	}
	defer shutdown(context.Background())
}

func TestBuildMeterProvider_Stdout(t *testing.T) {
	res := resource.Default()
	mp, shutdown, err := BuildMeterProvider(
		context.Background(), "stdout", "", false, 0, res,
	)
	if err != nil {
		t.Fatalf("BuildMeterProvider(stdout) error: %v", err)
	}
	if mp == nil {
		t.Fatal("expected non-nil MeterProvider")
	}
	defer shutdown(context.Background())
}

func TestBuildMeterProvider_OTLP_EmptyEndpoint(t *testing.T) {
	res := resource.Default()
	_, _, err := BuildMeterProvider(
		context.Background(), "otlp", "", false, 0, res,
	)
	if err == nil {
		t.Fatal("expected error for empty OTLP endpoint")
	}
}