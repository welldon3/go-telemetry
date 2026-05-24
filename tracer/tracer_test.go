package tracer

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func newTestProvider() (*sdktrace.TracerProvider, *tracetest.InMemoryExporter) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exp),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	return tp, exp
}

func TestSpanCreation(t *testing.T) {
	tp, exp := newTestProvider()
	tr := New(tp.Tracer("test"))

	ctx, span := tr.Start(context.Background(), "my-op")
	defer span.End()

	// Context must carry the active span.
	active := oteltrace.SpanFromContext(ctx)
	if !active.SpanContext().IsValid() {
		t.Fatal("context should carry a valid span")
	}

	span.End()

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if got := spans[0].Name; got != "my-op" {
		t.Errorf("span name: want %q, got %q", "my-op", got)
	}
}

func TestSpanAttributes_AllTypes(t *testing.T) {
	tp, exp := newTestProvider()
	tr := New(tp.Tracer("test"))

	_, span := tr.Start(context.Background(), "attrs")
	span.SetAttribute("str", "hello")
	span.SetAttribute("int", 42)
	span.SetAttribute("int64", int64(99))
	span.SetAttribute("float", 3.14)
	span.SetAttribute("bool", true)
	span.SetAttribute("unknown", struct{}{}) // falls back to fmt.Sprintf("%v", value)
	span.End()

	spans := exp.GetSpans()
	if len(spans) == 0 {
		t.Fatal("no spans captured")
	}

	want := map[string]any{
		"str":     "hello",
		"int":     int64(42),
		"int64":   int64(99),
		"float":   3.14,
		"bool":    true,
		"unknown": "{}", // fmt.Sprintf("%v", struct{}{}),
	}
	got := make(map[string]any)
	for _, attr := range spans[0].Attributes {
		got[string(attr.Key)] = attr.Value.AsInterface()
	}
	for k, wantVal := range want {
		if got[k] != wantVal {
			t.Errorf("attr %q: want %v (%T), got %v (%T)", k, wantVal, wantVal, got[k], got[k])
		}
	}
}

func TestSpanAddEvent(t *testing.T) {
	tp, exp := newTestProvider()
	tr := New(tp.Tracer("test"))

	_, span := tr.Start(context.Background(), "op")
	span.AddEvent("cache-miss")
	span.End()

	spans := exp.GetSpans()
	if len(spans) == 0 {
		t.Fatal("no spans captured")
	}
	if len(spans[0].Events) != 1 || spans[0].Events[0].Name != "cache-miss" {
		t.Errorf("expected event 'cache-miss', got %+v", spans[0].Events)
	}
}

func TestSpanContext(t *testing.T) {
	tp, _ := newTestProvider()
	tr := New(tp.Tracer("test"))

	_, span := tr.Start(context.Background(), "op")
	sc := span.SpanContext()
	if !sc.IsValid() {
		t.Error("SpanContext() should return a valid span context")
	}
	span.End()
}

func TestWithLink_AttachesLink(t *testing.T) {
	tp, exp := newTestProvider()
	tr := New(tp.Tracer("test"))

	// Create a "producer" span that will be linked from the consumer.
	producerCtx, producerSpan := tr.Start(context.Background(), "producer")
	producerSpan.End()

	// Create a "consumer" span linked to the producer.
	_, consumerSpan := tr.Start(context.Background(), "consumer", WithLink(producerCtx))
	consumerSpan.End()

	spans := exp.GetSpans()
	var consumer *tracetest.SpanStub
	for i := range spans {
		if spans[i].Name == "consumer" {
			consumer = &spans[i]
			break
		}
	}
	if consumer == nil {
		t.Fatal("consumer span not found")
	}
	if len(consumer.Links) != 1 {
		t.Fatalf("expected 1 link on consumer span, got %d", len(consumer.Links))
	}
	want := spans[0].SpanContext.TraceID()
	got := consumer.Links[0].SpanContext.TraceID()
	if want != got {
		t.Errorf("link trace ID: want %v, got %v", want, got)
	}
}

func TestWithLink_NoSpan_NoLink(t *testing.T) {
	tp, exp := newTestProvider()
	tr := New(tp.Tracer("test"))

	// Background context has no span — WithLink should be silently ignored.
	_, span := tr.Start(context.Background(), "op", WithLink(context.Background()))
	span.End()

	spans := exp.GetSpans()
	if len(spans) == 0 {
		t.Fatal("no spans captured")
	}
	if len(spans[0].Links) != 0 {
		t.Errorf("expected no links when context has no span, got %d", len(spans[0].Links))
	}
}

func TestSpanOptions(t *testing.T) {
	tp, exp := newTestProvider()
	tr := New(tp.Tracer("test"))

	_, span := tr.Start(context.Background(), "db.query",
		WithHTTPRoute("/api/v1/orders"),
		WithDB("postgres", "SELECT 1"),
		WithSpanKind(oteltrace.SpanKindClient),
	)
	span.End()

	spans := exp.GetSpans()
	if len(spans) == 0 {
		t.Fatal("no spans captured")
	}
	s := spans[0]
	if s.SpanKind != oteltrace.SpanKindClient {
		t.Errorf("span kind: want Client, got %v", s.SpanKind)
	}
	attrs := make(map[string]string)
	for _, a := range s.Attributes {
		attrs[string(a.Key)] = a.Value.AsString()
	}
	if attrs["http.route"] != "/api/v1/orders" {
		t.Errorf("http.route: want %q, got %q", "/api/v1/orders", attrs["http.route"])
	}
	if attrs["db.system"] != "postgres" {
		t.Errorf("db.system: want %q, got %q", "postgres", attrs["db.system"])
	}
}
