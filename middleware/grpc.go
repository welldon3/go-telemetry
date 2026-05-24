package middleware

import (
	"context"
	"fmt"
	"os"
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

// GRPCConfig configures the gRPC server interceptors.
type GRPCConfig struct {
	Tracer     tracer.Tracer
	Meter      meter.Meter                   // optional
	Propagator propagation.TextMapPropagator // defaults to W3C TraceContext + Baggage
}

type metadataCarrier metadata.MD

func (c metadataCarrier) Get(key string) string {
	vals := metadata.MD(c).Get(key)
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

func (c metadataCarrier) Set(key, value string) {
	metadata.MD(c).Set(key, value)
}

func (c metadataCarrier) Keys() []string {
	md := metadata.MD(c)
	keys := make([]string, 0, len(md))
	for k := range md {
		keys = append(keys, k)
	}
	return keys
}

// UnaryServerInterceptor creates a server span per RPC, records
// grpc.server.request.duration, and marks non-OK statuses as errors.
func UnaryServerInterceptor(cfg GRPCConfig) grpc.UnaryServerInterceptor {
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
			"grpc.server.request.duration",
			"Duration of gRPC server calls",
			"ms",
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "go-telemetry/middleware: grpc histogram: %v : metrics disabled\n", err)
			cfg.Meter = nil
		} else {
			reqCounter, err = cfg.Meter.Counter(
				"grpc.server.request.total",
				"Total number of gRPC server calls",
			)
			if err != nil {
				fmt.Fprintf(os.Stderr, "go-telemetry/middleware: grpc counter: %v : metrics disabled\n", err)
				reqDuration = nil
				cfg.Meter = nil
			}
		}
	}

	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()

		// Extract trace context from incoming gRPC metadata.
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			ctx = cfg.Propagator.Extract(ctx, metadataCarrier(md))
		}

		ctx, span := cfg.Tracer.Start(ctx, info.FullMethod,
			tracer.WithSpanKind(oteltrace.SpanKindServer),
			tracer.WithAttributes(attribute.String("rpc.system", "grpc")),
			tracer.WithAttributes(attribute.String("rpc.method", info.FullMethod)),
		)
		defer span.End()

		resp, err := handler(ctx, req)

		code := codes.OK
		if err != nil {
			code = status.Code(err)
			span.SetError(err)
		}

		duration := float64(time.Since(start).Milliseconds())
		attrs := []attribute.KeyValue{
			attribute.String("rpc.method", info.FullMethod),
			attribute.String("rpc.grpc.status_code", code.String()),
		}
		span.SetAttribute("rpc.grpc.status_code", code.String())

		if reqDuration != nil {
			reqDuration.Record(ctx, duration, attrs...)
		}
		if reqCounter != nil {
			reqCounter.Add(ctx, 1, attrs...)
		}

		return resp, err
	}
}

// StreamServerInterceptor is the streaming counterpart of UnaryServerInterceptor.
func StreamServerInterceptor(cfg GRPCConfig) grpc.StreamServerInterceptor {
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
			"grpc.server.stream.duration",
			"Duration of gRPC server streams",
			"ms",
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "go-telemetry/middleware: grpc stream histogram: %v : metrics disabled\n", err)
			cfg.Meter = nil
		} else {
			reqCounter, err = cfg.Meter.Counter(
				"grpc.server.stream.total",
				"Total number of gRPC server streams",
			)
			if err != nil {
				fmt.Fprintf(os.Stderr, "go-telemetry/middleware: grpc stream counter: %v : metrics disabled\n", err)
				reqDuration = nil
				cfg.Meter = nil
			}
		}
	}

	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := ss.Context()
		start := time.Now()

		if md, ok := metadata.FromIncomingContext(ctx); ok {
			ctx = cfg.Propagator.Extract(ctx, metadataCarrier(md))
		}

		ctx, span := cfg.Tracer.Start(ctx, info.FullMethod,
			tracer.WithSpanKind(oteltrace.SpanKindServer),
			tracer.WithAttributes(
				attribute.String("rpc.system", "grpc"),
				attribute.String("rpc.method", info.FullMethod),
				attribute.Bool("rpc.grpc.stream", true),
			),
		)
		defer span.End()

		wrapped := &wrappedStream{ServerStream: ss, ctx: ctx}
		err := handler(srv, wrapped)

		code := codes.OK
		if err != nil {
			code = status.Code(err)
			span.SetError(err)
		}

		duration := float64(time.Since(start).Milliseconds())
		attrs := []attribute.KeyValue{
			attribute.String("rpc.method", info.FullMethod),
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

// wrappedStream injects the span-carrying context into a ServerStream.
type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context { return w.ctx }