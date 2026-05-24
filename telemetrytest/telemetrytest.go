// Package telemetrytest provides in-memory test doubles for go-telemetry.
//
// Use SpanRecorder to assert on spans created by instrumented code:
//
//	rec, tr := telemetrytest.NewSpanRecorder()
//	// pass tr to the code under test
//	span, ok := rec.SpanNamed("processOrder")
//	if !ok { t.Fatal("span not found") }
//
// Use NoopLogger() to satisfy Logger parameters in tests that don't need
// log output.
package telemetrytest

import (
	"context"

	"github.com/welldon3/go-telemetry/logger"
	"github.com/welldon3/go-telemetry/tracer"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// SpanRecorder captures completed spans for test assertions.
type SpanRecorder struct {
	exp *tracetest.InMemoryExporter
}

// NewSpanRecorder creates an in-memory span recorder and a ready-to-use Tracer.
// All spans are exported synchronously : no need to flush.
func NewSpanRecorder() (*SpanRecorder, tracer.Tracer) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exp),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	return &SpanRecorder{exp: exp}, tracer.New(tp.Tracer("test"))
}

// Spans returns all completed spans in the order they finished.
func (r *SpanRecorder) Spans() tracetest.SpanStubs {
	return r.exp.GetSpans()
}

// SpanNamed returns the first completed span with the given name.
// Returns false if no such span exists.
func (r *SpanRecorder) SpanNamed(name string) (tracetest.SpanStub, bool) {
	for _, s := range r.exp.GetSpans() {
		if s.Name == name {
			return s, true
		}
	}
	return tracetest.SpanStub{}, false
}

// Reset clears all recorded spans.
func (r *SpanRecorder) Reset() {
	r.exp.Reset()
}

// NoopLogger returns a Logger that discards all output.
// Use it to satisfy Logger parameters in tests that don't care about log output.
func NoopLogger() logger.Logger {
	return &noopLogger{}
}

type noopLogger struct{}

func (n *noopLogger) Debug(_ context.Context, _ string, _ ...any) {}
func (n *noopLogger) Info(_ context.Context, _ string, _ ...any)  {}
func (n *noopLogger) Warn(_ context.Context, _ string, _ ...any)  {}
func (n *noopLogger) Error(_ context.Context, _ string, _ ...any) {}
func (n *noopLogger) With(_ ...any) logger.Logger                  { return n }
func (n *noopLogger) WithGroup(_ string) logger.Logger             { return n }
