package middleware

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/welldon3/go-telemetry/meter"
	"github.com/welldon3/go-telemetry/tracer"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// GRPCClientConfig configures the gRPC client interceptors.
type GRPCClientConfig struct {
	Tracer     tracer.Tracer
	Meter      meter.Meter                   // optional
	Propagator propagation.TextMapPropagator // defaults to W3C TraceContext + Baggage
}

// UnaryClientInterceptor creates a client span per RPC, injects trace context
// into outgoing metadata, and marks non-OK statuses as errors.
func UnaryClientInterceptor(cfg GRPCClientConfig) grpc.UnaryClientInterceptor {
	if cfg.Propagator == nil {
		cfg.Propagator = propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		)
	}

	var reqDuration meter.Histogram
	var reqCounter meter.Counter
	if cfg.Meter != nil {
		var err error
		reqDuration, err = cfg.Meter.Histogram(
			"grpc.client.request.duration",
			"Duration of outgoing gRPC client calls",
			"ms",
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "go-telemetry/middleware: grpc client histogram: %v : metrics disabled\n", err)
		} else {
			reqCounter, err = cfg.Meter.Counter(
				"grpc.client.request.total",
				"Total number of outgoing gRPC client calls",
			)
			if err != nil {
				fmt.Fprintf(os.Stderr, "go-telemetry/middleware: grpc client counter: %v : metrics disabled\n", err)
				reqDuration = nil
			}
		}
	}

	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		start := time.Now()

		ctx, span := cfg.Tracer.Start(ctx, method,
			tracer.WithSpanKind(oteltrace.SpanKindClient),
			tracer.WithAttributes(
				attribute.String("rpc.system", "grpc"),
				attribute.String("rpc.method", method),
			),
		)
		defer span.End()

		ctx = injectGRPCMetadata(ctx, cfg.Propagator)

		err := invoker(ctx, method, req, reply, cc, opts...)

		code := codes.OK
		if err != nil {
			code = status.Code(err)
			span.SetError(err)
		}

		duration := float64(time.Since(start).Milliseconds())
		attrs := []attribute.KeyValue{
			attribute.String("rpc.method", method),
			attribute.String("rpc.grpc.status_code", code.String()),
		}
		span.SetAttribute("rpc.grpc.status_code", code.String())

		if reqDuration != nil {
			reqDuration.Record(ctx, duration, attrs...)
		}
		if reqCounter != nil {
			reqCounter.Add(ctx, 1, attrs...)
		}

		return err
	}
}

// StreamClientInterceptor creates a client span that lives for the full
// duration of the stream and injects trace context into outgoing metadata.
func StreamClientInterceptor(cfg GRPCClientConfig) grpc.StreamClientInterceptor {
	if cfg.Propagator == nil {
		cfg.Propagator = propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		)
	}

	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		ctx, span := cfg.Tracer.Start(ctx, method,
			tracer.WithSpanKind(oteltrace.SpanKindClient),
			tracer.WithAttributes(
				attribute.String("rpc.system", "grpc"),
				attribute.String("rpc.method", method),
				attribute.Bool("rpc.grpc.stream", true),
			),
		)

		ctx = injectGRPCMetadata(ctx, cfg.Propagator)

		stream, err := streamer(ctx, desc, cc, method, opts...)
		if err != nil {
			span.SetError(err)
			span.End()
			return nil, err
		}

		return &wrappedClientStream{ClientStream: stream, span: span}, nil
	}
}

type wrappedClientStream struct {
	grpc.ClientStream
	span    tracer.Span
	endOnce sync.Once
}

func (w *wrappedClientStream) endSpan() {
	w.endOnce.Do(w.span.End)
}

func (w *wrappedClientStream) SendMsg(m any) error {
	err := w.ClientStream.SendMsg(m)
	if err != nil {
		w.span.SetError(err)
		w.endSpan()
	}
	return err
}

func (w *wrappedClientStream) RecvMsg(m any) error {
	err := w.ClientStream.RecvMsg(m)
	if err != nil {
		if err != io.EOF {
			w.span.SetError(err)
		}
		w.endSpan()
	}
	return err
}

func injectGRPCMetadata(ctx context.Context, p propagation.TextMapPropagator) context.Context {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		md = metadata.New(nil)
	} else {
		md = md.Copy()
	}
	p.Inject(ctx, metadataCarrier(md))
	return metadata.NewOutgoingContext(ctx, md)
}