// Package tracer wraps the OpenTelemetry tracer with a friendlier API
// and convenience span option helpers.
package tracer

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// WithLink links this span to the span in ctx.
// Useful for async handoffs: queue producers linking to consumer spans.
func WithLink(ctx context.Context) SpanOption {
	return func(o *spanOptions) {
		sc := oteltrace.SpanFromContext(ctx).SpanContext()
		if sc.IsValid() {
			o.otelOpts = append(o.otelOpts, oteltrace.WithLinks(oteltrace.Link{SpanContext: sc}))
		}
	}
}

type Tracer interface {
	Start(ctx context.Context, spanName string, opts ...SpanOption) (context.Context, Span)
}

type Span interface {
	End()
	SetError(err error)
	SetAttribute(key string, value any)
	AddEvent(name string, attrs ...attribute.KeyValue)
	SpanContext() oteltrace.SpanContext
}

// SpanOption configures a span at creation time.
type SpanOption func(*spanOptions)

type spanOptions struct {
	otelOpts []oteltrace.SpanStartOption
}

// WithHTTPRoute annotates the span with the matched HTTP route pattern.
func WithHTTPRoute(route string) SpanOption {
	return func(o *spanOptions) {
		o.otelOpts = append(o.otelOpts, oteltrace.WithAttributes(
			attribute.String("http.route", route),
		))
	}
}

// WithDB annotates the span with database system and statement.
func WithDB(system, statement string) SpanOption {
	return func(o *spanOptions) {
		o.otelOpts = append(o.otelOpts, oteltrace.WithAttributes(
			attribute.String("db.system", system),
			attribute.String("db.statement", statement),
		))
	}
}

// WithAttributes adds arbitrary OTel attributes to the span.
func WithAttributes(attrs ...attribute.KeyValue) SpanOption {
	return func(o *spanOptions) {
		o.otelOpts = append(o.otelOpts, oteltrace.WithAttributes(attrs...))
	}
}

// WithSpanKind sets the span kind (Client, Server, Producer, Consumer, Internal).
func WithSpanKind(kind oteltrace.SpanKind) SpanOption {
	return func(o *spanOptions) {
		o.otelOpts = append(o.otelOpts, oteltrace.WithSpanKind(kind))
	}
}

type otelTracer struct {
	inner oteltrace.Tracer
}

// New wraps an OTel tracer.
func New(t oteltrace.Tracer) Tracer {
	return &otelTracer{inner: t}
}

func (t *otelTracer) Start(ctx context.Context, spanName string, opts ...SpanOption) (context.Context, Span) {
	so := &spanOptions{}
	for _, o := range opts {
		o(so)
	}
	ctx, s := t.inner.Start(ctx, spanName, so.otelOpts...)
	return ctx, &otelSpan{inner: s}
}

type otelSpan struct {
	inner oteltrace.Span
}

func (s *otelSpan) End() { s.inner.End() }

func (s *otelSpan) SetError(err error) {
	if err == nil {
		return
	}
	s.inner.SetAttributes(attribute.Bool("error", true))
	s.inner.RecordError(err)
	s.inner.SetStatus(codes.Error, err.Error())
}

func (s *otelSpan) SetAttribute(key string, value any) {
	switch v := value.(type) {
	case string:
		s.inner.SetAttributes(attribute.String(key, v))
	case int:
		s.inner.SetAttributes(attribute.Int(key, v))
	case int64:
		s.inner.SetAttributes(attribute.Int64(key, v))
	case float64:
		s.inner.SetAttributes(attribute.Float64(key, v))
	case bool:
		s.inner.SetAttributes(attribute.Bool(key, v))
	default:
		s.inner.SetAttributes(attribute.String(key, fmt.Sprintf("%v", value)))
	}
}

func (s *otelSpan) AddEvent(name string, attrs ...attribute.KeyValue) {
	s.inner.AddEvent(name, oteltrace.WithAttributes(attrs...))
}

func (s *otelSpan) SpanContext() oteltrace.SpanContext {
	return s.inner.SpanContext()
}
