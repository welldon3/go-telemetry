package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func TestHTTPClientTransport_CreatesClientSpan(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tr := newTestTracer(exp)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewHTTPClient(HTTPClientConfig{Tracer: tr})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/ping", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if got := spans[0].SpanKind; got != oteltrace.SpanKindClient {
		t.Errorf("span kind: want Client, got %v", got)
	}
	if got := spans[0].Name; got != "GET /ping" {
		t.Errorf("span name: want %q, got %q", "GET /ping", got)
	}
}

func TestHTTPClientTransport_InjectsTraceparentHeader(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tr := newTestTracer(exp)

	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("Traceparent")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Start a parent span so there is a valid trace context to inject.
	ctx, parentSpan := tr.Start(context.Background(), "parent")

	client := NewHTTPClient(HTTPClientConfig{Tracer: tr})
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()
	parentSpan.End()

	if gotHeader == "" {
		t.Error("traceparent header was not injected into the outgoing request")
	}
}

func TestHTTPClientTransport_MarksErrorOn5xx(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tr := newTestTracer(exp)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewHTTPClient(HTTPClientConfig{Tracer: tr})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()

	spans := exp.GetSpans()
	if len(spans) == 0 {
		t.Fatal("no spans captured")
	}
	if spans[0].Status.Code.String() != "Error" {
		t.Errorf("expected Error status for 5xx response, got %q", spans[0].Status.Code.String())
	}
}

func TestHTTPClientTransport_NoErrorOn2xx(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tr := newTestTracer(exp)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	client := NewHTTPClient(HTTPClientConfig{Tracer: tr})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+"/", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()

	spans := exp.GetSpans()
	if len(spans) == 0 {
		t.Fatal("no spans captured")
	}
	if spans[0].Status.Code.String() == "Error" {
		t.Error("2xx response should not mark span as error")
	}
}