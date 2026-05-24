package propagation

import (
	"context"
	"net/http"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func startSpan(t *testing.T) (context.Context, func()) {
	t.Helper()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	ctx, span := tp.Tracer("test").Start(context.Background(), "op")
	return ctx, func() { span.End() }
}

func TestTraceID_WithSpan(t *testing.T) {
	ctx, end := startSpan(t)
	defer end()

	id := TraceID(ctx)
	if id == "" {
		t.Error("TraceID should be non-empty when a span is active")
	}
	if len(id) != 32 {
		t.Errorf("TraceID should be 32 hex chars, got %q (len %d)", id, len(id))
	}
}

func TestTraceID_NoSpan(t *testing.T) {
	if got := TraceID(context.Background()); got != "" {
		t.Errorf("TraceID should be empty without a span, got %q", got)
	}
}

func TestSpanID_WithSpan(t *testing.T) {
	ctx, end := startSpan(t)
	defer end()

	id := SpanID(ctx)
	if id == "" {
		t.Error("SpanID should be non-empty when a span is active")
	}
	if len(id) != 16 {
		t.Errorf("SpanID should be 16 hex chars, got %q (len %d)", id, len(id))
	}
}

func TestSpanID_NoSpan(t *testing.T) {
	if got := SpanID(context.Background()); got != "" {
		t.Errorf("SpanID should be empty without a span, got %q", got)
	}
}

func TestIsSampled(t *testing.T) {
	ctx, end := startSpan(t) // AlwaysSample
	defer end()

	if !IsSampled(ctx) {
		t.Error("IsSampled should be true with AlwaysSample")
	}
}

func TestIsSampled_NoSpan(t *testing.T) {
	if IsSampled(context.Background()) {
		t.Error("IsSampled should be false without a span")
	}
}

func TestIsRecording(t *testing.T) {
	ctx, end := startSpan(t)
	defer end()

	if !IsRecording(ctx) {
		t.Error("IsRecording should be true for an active span")
	}
}

func TestIsRecording_NoSpan(t *testing.T) {
	if IsRecording(context.Background()) {
		t.Error("IsRecording should be false without a span")
	}
}

func TestInjectHeaders_WritesTraceparent(t *testing.T) {
	ctx, end := startSpan(t)
	defer end()

	h := make(http.Header)
	InjectHeaders(ctx, h)

	if got := h.Get("Traceparent"); got == "" {
		t.Error("InjectHeaders should write a traceparent header when a span is active")
	}
}

func TestInjectHeaders_NoSpan_NoHeader(t *testing.T) {
	h := make(http.Header)
	InjectHeaders(context.Background(), h)

	if got := h.Get("Traceparent"); got != "" {
		t.Errorf("InjectHeaders with no active span should not write traceparent, got %q", got)
	}
}

func TestExtractHeaders_RestoresTraceContext(t *testing.T) {
	// Inject a known trace context via a real span.
	ctx, end := startSpan(t)
	defer end()

	h := make(http.Header)
	InjectHeaders(ctx, h)

	traceID := TraceID(ctx)

	// Extract into a fresh background context.
	restored := ExtractHeaders(context.Background(), h)
	if got := TraceID(restored); got != traceID {
		t.Errorf("ExtractHeaders: restored trace ID %q, want %q", got, traceID)
	}
}