package tracer

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestErrorSampler_ForcesRecordOnError checks that the sampler overrides
// a NeverSample base when the "error" attribute is true.
func TestErrorSampler_ForcesRecordOnError(t *testing.T) {
	sampler := NewErrorSampler(sdktrace.NeverSample())

	result := sampler.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: context.Background(),
		Attributes:    []attribute.KeyValue{attribute.Bool("error", true)},
	})

	if result.Decision != sdktrace.RecordAndSample {
		t.Errorf("expected RecordAndSample, got %v", result.Decision)
	}
}

// TestErrorSampler_DelegatesToBaseWhenNoError checks delegation to the base sampler.
func TestErrorSampler_DelegatesToBaseWhenNoError(t *testing.T) {
	sampler := NewErrorSampler(sdktrace.NeverSample())

	result := sampler.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: context.Background(),
		Attributes:    []attribute.KeyValue{attribute.String("http.route", "/ping")},
	})

	if result.Decision != sdktrace.Drop {
		t.Errorf("expected Drop, got %v", result.Decision)
	}
}

// TestErrorSampler_IgnoresFalseErrorAttribute ensures error=false does not force sampling.
func TestErrorSampler_IgnoresFalseErrorAttribute(t *testing.T) {
	sampler := NewErrorSampler(sdktrace.NeverSample())

	result := sampler.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: context.Background(),
		Attributes:    []attribute.KeyValue{attribute.Bool("error", false)},
	})

	if result.Decision != sdktrace.Drop {
		t.Errorf("expected Drop for error=false, got %v", result.Decision)
	}
}

// TestErrorSampler_Description verifies the description contains base info.
func TestErrorSampler_Description(t *testing.T) {
	sampler := NewErrorSampler(sdktrace.AlwaysSample())
	desc := sampler.Description()
	if desc == "" {
		t.Error("expected non-empty description")
	}
}

// TestSetError_SetsErrorAttribute verifies that SetError marks the span with error=true.
func TestSetError_SetsErrorAttribute(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exp),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	_, s := tp.Tracer("test").Start(context.Background(), "op")
	wrapped := &otelSpan{inner: s}
	wrapped.SetError(errors.New("boom"))
	wrapped.End()

	spans := exp.GetSpans()
	if len(spans) == 0 {
		t.Fatal("no spans captured")
	}

	found := false
	for _, attr := range spans[0].Attributes {
		if string(attr.Key) == "error" && attr.Value.AsBool() {
			found = true
			break
		}
	}
	if !found {
		t.Error("SetError should set error=true attribute on the span")
	}
}

// TestSetError_Nil verifies that SetError with nil does nothing.
func TestSetError_Nil(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))

	_, s := tp.Tracer("test").Start(context.Background(), "op")
	wrapped := &otelSpan{inner: s}
	wrapped.SetError(nil)
	wrapped.End()

	spans := exp.GetSpans()
	for _, attr := range spans[0].Attributes {
		if string(attr.Key) == "error" {
			t.Error("SetError(nil) should not set error attribute")
		}
	}
}
