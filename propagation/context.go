// Package propagation provides helpers for working with trace context
// in Go's context.Context.
package propagation

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

var defaultPropagator = propagation.NewCompositeTextMapPropagator(
	propagation.TraceContext{},
	propagation.Baggage{},
)

// TraceID returns the trace ID of the active span, or "" if none.
func TraceID(ctx context.Context) string {
	sc := trace.SpanFromContext(ctx).SpanContext()
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}

// SpanID returns the span ID of the active span, or "" if none.
func SpanID(ctx context.Context) string {
	sc := trace.SpanFromContext(ctx).SpanContext()
	if !sc.IsValid() {
		return ""
	}
	return sc.SpanID().String()
}

// IsSampled reports whether the current span is being sampled.
func IsSampled(ctx context.Context) bool {
	return trace.SpanFromContext(ctx).SpanContext().IsSampled()
}

// IsRecording reports whether the current span is recording events.
func IsRecording(ctx context.Context) bool {
	return trace.SpanFromContext(ctx).IsRecording()
}

// InjectHeaders writes W3C trace context from ctx into h (traceparent, baggage).
func InjectHeaders(ctx context.Context, h http.Header) {
	defaultPropagator.Inject(ctx, propagation.HeaderCarrier(h))
}

// ExtractHeaders restores trace context from h into a new context.
func ExtractHeaders(ctx context.Context, h http.Header) context.Context {
	return defaultPropagator.Extract(ctx, propagation.HeaderCarrier(h))
}

// SetBaggage returns a new context with key=value added to OTel Baggage.
// Invalid keys or values (per the W3C baggage spec) are silently ignored.
func SetBaggage(ctx context.Context, key, value string) context.Context {
	m, err := baggage.NewMember(key, value)
	if err != nil {
		return ctx
	}
	b, err := baggage.FromContext(ctx).SetMember(m)
	if err != nil {
		return ctx
	}
	return baggage.ContextWithBaggage(ctx, b)
}

// GetBaggage returns the baggage value for key, or "" if absent.
func GetBaggage(ctx context.Context, key string) string {
	return baggage.FromContext(ctx).Member(key).Value()
}

// BaggageMap returns all baggage members in ctx as a plain map.
func BaggageMap(ctx context.Context) map[string]string {
	members := baggage.FromContext(ctx).Members()
	if len(members) == 0 {
		return nil
	}
	out := make(map[string]string, len(members))
	for _, m := range members {
		out[m.Key()] = m.Value()
	}
	return out
}
