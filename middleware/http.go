// Package middleware provides HTTP and gRPC server/client instrumentation.
package middleware

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/welldon3/go-telemetry/logger"
	"github.com/welldon3/go-telemetry/meter"
	"github.com/welldon3/go-telemetry/tracer"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
)

const httpServerErrorThreshold = 500

type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    int64
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.written += int64(n)
	return n, err
}

// HTTPConfig configures the HTTP middleware.
type HTTPConfig struct {
	Tracer     tracer.Tracer
	Logger     logger.Logger                  // optional
	Meter      meter.Meter                    // optional
	Propagator propagation.TextMapPropagator  // defaults to W3C TraceContext + Baggage

	// RouteFunc extracts the matched route pattern for the span name.
	// Defaults to r.URL.Path.
	// Go 1.23+: func(r *http.Request) string { return r.Pattern }
	// chi:      func(r *http.Request) string { return chi.RouteContext(r.Context()).RoutePattern() }
	RouteFunc func(r *http.Request) string
}

// HTTP returns an http.Handler middleware that creates a server span per
// request, records http.server.request.duration, and marks 5xx as errors.
func HTTP(cfg HTTPConfig) func(next http.Handler) http.Handler {
	if cfg.Propagator == nil {
		cfg.Propagator = propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		)
	}
	if cfg.RouteFunc == nil {
		cfg.RouteFunc = func(r *http.Request) string { return r.URL.Path }
	}

	var reqDuration meter.Histogram
	var reqCounter meter.Counter
	if cfg.Meter != nil {
		var err error
		reqDuration, err = cfg.Meter.Histogram(
			"http.server.request.duration",
			"Duration of HTTP server requests",
			"ms",
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "go-telemetry/middleware: create histogram: %v : metrics disabled\n", err)
			cfg.Meter = nil
		} else {
			reqCounter, err = cfg.Meter.Counter(
				"http.server.request.total",
				"Total number of HTTP server requests",
			)
			if err != nil {
				fmt.Fprintf(os.Stderr, "go-telemetry/middleware: create counter: %v : metrics disabled\n", err)
				reqDuration = nil
				cfg.Meter = nil
			}
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Extract incoming trace context.
			ctx := cfg.Propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			route := cfg.RouteFunc(r)
			spanName := r.Method + " " + route

			ctx, span := cfg.Tracer.Start(ctx, spanName,
				tracer.WithSpanKind(oteltrace.SpanKindServer),
				tracer.WithHTTPRoute(route),
				tracer.WithAttributes(
					attribute.String("http.method", r.Method),
					attribute.String("http.url", r.URL.String()),
					attribute.String("http.scheme", scheme(r)),
					attribute.String("net.peer.addr", r.RemoteAddr),
				),
			)
			defer span.End()

			rw := newResponseWriter(w)
			next.ServeHTTP(rw, r.WithContext(ctx))

			duration := float64(time.Since(start).Milliseconds())
			statusCode := rw.statusCode

			attrs := []attribute.KeyValue{
				attribute.String("http.method", r.Method),
				attribute.String("http.route", route),
				attribute.Int("http.status_code", statusCode),
			}

			span.SetAttribute("http.status_code", statusCode)
			span.SetAttribute("http.response_content_length", rw.written)

			if statusCode >= httpServerErrorThreshold {
				span.SetError(fmt.Errorf("HTTP %d", statusCode))
			}

			if reqDuration != nil {
				reqDuration.Record(ctx, duration, attrs...)
			}
			if reqCounter != nil {
				reqCounter.Add(ctx, 1, attrs...)
			}

			if cfg.Logger != nil {
				cfg.Logger.Info(ctx, "http request",
					"method", r.Method,
					"route", route,
					"status", statusCode,
					"duration_ms", duration,
					"bytes", rw.written,
				)
			}
		})
	}
}

func scheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}
