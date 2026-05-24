package middleware

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/welldon3/go-telemetry/meter"
	"github.com/welldon3/go-telemetry/tracer"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// HTTPClientConfig configures the HTTP client transport.
type HTTPClientConfig struct {
	Tracer     tracer.Tracer
	Meter      meter.Meter                   // optional
	Propagator propagation.TextMapPropagator // defaults to W3C TraceContext + Baggage
	Transport  http.RoundTripper             // defaults to http.DefaultTransport
}

// HTTPClientTransport returns an http.RoundTripper that creates a client span
// per request, injects traceparent, and marks 5xx as errors.
func HTTPClientTransport(cfg HTTPClientConfig) http.RoundTripper {
	if cfg.Propagator == nil {
		cfg.Propagator = propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		)
	}
	if cfg.Transport == nil {
		cfg.Transport = http.DefaultTransport
	}

	var reqDuration meter.Histogram
	if cfg.Meter != nil {
		var err error
		reqDuration, err = cfg.Meter.Histogram(
			"http.client.request.duration",
			"Duration of outgoing HTTP client requests",
			"ms",
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "go-telemetry/middleware: http client histogram: %v : metrics disabled\n", err)
		}
	}

	return &httpClientTransport{cfg: cfg, reqDuration: reqDuration}
}

// NewHTTPClient returns an *http.Client instrumented with HTTPClientTransport.
func NewHTTPClient(cfg HTTPClientConfig) *http.Client {
	return &http.Client{Transport: HTTPClientTransport(cfg)}
}

type httpClientTransport struct {
	cfg         HTTPClientConfig
	reqDuration meter.Histogram
}

func (t *httpClientTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	ctx := r.Context()

	ctx, span := t.cfg.Tracer.Start(ctx, r.Method+" "+r.URL.Path,
		tracer.WithSpanKind(oteltrace.SpanKindClient),
		tracer.WithAttributes(
			attribute.String("http.method", r.Method),
			attribute.String("http.url", r.URL.String()),
			attribute.String("net.peer.host", r.URL.Host),
		),
	)
	defer span.End()

	r = r.Clone(ctx) // don't mutate the caller's headers
	t.cfg.Propagator.Inject(ctx, propagation.HeaderCarrier(r.Header))

	start := time.Now()
	resp, err := t.cfg.Transport.RoundTrip(r)
	duration := float64(time.Since(start).Milliseconds())

	if err != nil {
		span.SetError(err)
		return nil, err
	}

	span.SetAttribute("http.status_code", resp.StatusCode)
	if resp.StatusCode >= httpServerErrorThreshold {
		span.SetError(fmt.Errorf("HTTP %d", resp.StatusCode))
	}

	if t.reqDuration != nil {
		t.reqDuration.Record(ctx, duration,
			attribute.String("http.method", r.Method),
			attribute.String("net.peer.host", r.URL.Host),
			attribute.Int("http.status_code", resp.StatusCode),
		)
	}

	return resp, nil
}