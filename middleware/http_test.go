package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/welldon3/go-telemetry/tracer"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func newTestTracer(exp *tracetest.InMemoryExporter) tracer.Tracer {
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exp),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	return tracer.New(tp.Tracer("test"))
}

func TestHTTP_CreatesSpan(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tr := newTestTracer(exp)

	handler := HTTP(HTTPConfig{
		Tracer:    tr,
		RouteFunc: func(r *http.Request) string { return "/test" },
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if got := spans[0].Name; got != "GET /test" {
		t.Errorf("span name: want %q, got %q", "GET /test", got)
	}
	if got := spans[0].SpanKind; got != oteltrace.SpanKindServer {
		t.Errorf("span kind: want Server, got %v", got)
	}
}

func TestHTTP_MarksErrorOn5xx(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tr := newTestTracer(exp)

	handler := HTTP(HTTPConfig{
		Tracer:    tr,
		RouteFunc: func(r *http.Request) string { return "/err" },
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	req := httptest.NewRequest(http.MethodPost, "/err", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	spans := exp.GetSpans()
	if len(spans) == 0 {
		t.Fatal("no spans captured")
	}

	// Span status should be Error for 5xx.
	if spans[0].Status.Code.String() != "Error" {
		t.Errorf("expected Error status for 5xx, got %q", spans[0].Status.Code.String())
	}

	// error=true attribute should be set by SetError.
	found := false
	for _, attr := range spans[0].Attributes {
		if string(attr.Key) == "error" && attr.Value.AsBool() {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected error=true attribute on 5xx span")
	}
}

func TestHTTP_PropagatesIncomingTraceContext(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tr := newTestTracer(exp)

	handler := HTTP(HTTPConfig{
		Tracer:    tr,
		RouteFunc: func(r *http.Request) string { return "/propagate" },
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/propagate", nil)
	// Inject a fake W3C traceparent header.
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	spans := exp.GetSpans()
	if len(spans) == 0 {
		t.Fatal("no spans captured")
	}

	traceID := spans[0].SpanContext.TraceID().String()
	if traceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("expected trace ID from header, got %q", traceID)
	}
}

func TestHTTP_NoErrorOn2xx(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tr := newTestTracer(exp)

	handler := HTTP(HTTPConfig{
		Tracer:    tr,
		RouteFunc: func(r *http.Request) string { return "/ok" },
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	spans := exp.GetSpans()
	if len(spans) == 0 {
		t.Fatal("no spans captured")
	}
	if spans[0].Status.Code.String() == "Error" {
		t.Error("2xx response should not mark span as error")
	}
}
