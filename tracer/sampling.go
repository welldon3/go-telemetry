package tracer

import (
	oteltrace "go.opentelemetry.io/otel/trace"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// ErrorSampler wraps a base sampler and forces sampling when a span
// has recorded an error. This ensures that error traces are always
// captured regardless of the base sampling ratio.
type ErrorSampler struct {
	base sdktrace.Sampler
}

// NewErrorSampler returns a sampler that delegates to base for normal
// spans, but always samples spans that contain errors.
func NewErrorSampler(base sdktrace.Sampler) sdktrace.Sampler {
	return &ErrorSampler{base: base}
}

func (s *ErrorSampler) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	// Check if any attribute signals an error.
	for _, attr := range p.Attributes {
		if string(attr.Key) == "error" && attr.Value.AsBool() {
			return sdktrace.SamplingResult{
				Decision:   sdktrace.RecordAndSample,
				Tracestate: oteltrace.SpanContextFromContext(p.ParentContext).TraceState(),
			}
		}
	}
	return s.base.ShouldSample(p)
}

func (s *ErrorSampler) Description() string {
	return "ErrorSampler{base=" + s.base.Description() + "}"
}
