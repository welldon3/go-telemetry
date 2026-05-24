// Example: grpc demonstrates a fully instrumented gRPC server and client.
//
// This example defines a minimal Echo service without protobuf tooling,
// replacing the default codec with JSON via encoding.RegisterCodec.
// In a real project you would generate the service stub from a .proto file.
//
// What to observe:
//   - The client span and server span share the same trace_id.
//   - Server-side log entries automatically carry that trace_id.
//   - UnaryClientInterceptor injects traceparent into gRPC metadata.
//   - UnaryServerInterceptor extracts it and continues the trace.
//
// Run:
//
//	go run ./examples/grpc
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"time"

	telemetry "github.com/welldon3/go-telemetry"
	"github.com/welldon3/go-telemetry/logger"
	"github.com/welldon3/go-telemetry/middleware"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding"
)

// jsonCodec replaces the default protobuf codec with JSON so this example
// runs without any .proto files or protoc tooling.
// In production, remove this and use the standard proto codec.
type jsonCodec struct{}

func (jsonCodec) Marshal(v any) ([]byte, error)      { return json.Marshal(v) }
func (jsonCodec) Unmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }
func (jsonCodec) Name() string                       { return "proto" } // replaces default

func init() { encoding.RegisterCodec(jsonCodec{}) }

// ---------- Echo service definition (replaces proto-generated code) ----------

type echoRequest struct{ Message string }
type echoResponse struct{ Message string }

type echoServer interface {
	Echo(context.Context, *echoRequest) (*echoResponse, error)
}

type echoImpl struct{ log logger.Logger }

func (s *echoImpl) Echo(ctx context.Context, req *echoRequest) (*echoResponse, error) {
	// trace_id is in ctx, so this log entry is automatically correlated with
	// the client span that triggered this RPC.
	s.log.Info(ctx, "handling echo", "message", req.Message)
	return &echoResponse{Message: "echo: " + req.Message}, nil
}

var echoServiceDesc = grpc.ServiceDesc{
	ServiceName: "example.Echo",
	HandlerType: (*echoServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Echo",
			Handler: func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
				req := new(echoRequest)
				if err := dec(req); err != nil {
					return nil, err
				}
				handler := func(ctx context.Context, req any) (any, error) {
					return srv.(echoServer).Echo(ctx, req.(*echoRequest))
				}
				if interceptor == nil {
					return handler(ctx, req)
				}
				return interceptor(ctx, req, &grpc.UnaryServerInfo{
					Server:     srv,
					FullMethod: "/example.Echo/Echo",
				}, handler)
			},
		},
	},
}

type echoClient struct{ cc *grpc.ClientConn }

func (c *echoClient) Echo(ctx context.Context, req *echoRequest, opts ...grpc.CallOption) (*echoResponse, error) {
	resp := new(echoResponse)
	return resp, c.cc.Invoke(ctx, "/example.Echo/Echo", req, resp, opts...)
}

// ---------- main ----------

func main() {
	ctx := context.Background()

	tel, err := telemetry.New(ctx, telemetry.Config{
		ServiceName:    "grpc-example",
		ServiceVersion: "0.1.0",
		Environment:    "development",
		Exporter:       telemetry.ExporterStdout,
		SamplingRatio:  1.0,
	})
	if err != nil {
		log.Fatalf("telemetry init: %v", err)
	}
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tel.ForceFlush(shutCtx)
		_ = tel.Shutdown(shutCtx)
	}()

	lg := tel.Logger()
	tr := tel.Tracer()
	m := tel.Meter()

	// ---------- gRPC server ----------
	grpcSrv := grpc.NewServer(
		grpc.UnaryInterceptor(middleware.UnaryServerInterceptor(middleware.GRPCConfig{
			Tracer: tr,
			Meter:  m,
		})),
		grpc.StreamInterceptor(middleware.StreamServerInterceptor(middleware.GRPCConfig{
			Tracer: tr,
			Meter:  m,
		})),
	)
	grpcSrv.RegisterService(&echoServiceDesc, &echoImpl{log: lg})

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	go func() {
		if err := grpcSrv.Serve(lis); err != nil {
			log.Printf("server stopped: %v", err)
		}
	}()
	defer grpcSrv.GracefulStop()

	time.Sleep(20 * time.Millisecond) // let server start

	// ---------- gRPC client ----------
	conn, err := grpc.NewClient("localhost:50051",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(middleware.UnaryClientInterceptor(middleware.GRPCClientConfig{
			Tracer: tr,
			Meter:  m,
		})),
	)
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	client := &echoClient{cc: conn}

	// Wrap all RPCs in a parent span — the trace_id propagates to the server
	// via gRPC metadata (traceparent header) and appears in server-side logs.
	ctx, span := tr.Start(ctx, "grpc-demo")
	defer span.End()

	lg.Info(ctx, "starting RPC calls")

	for i := range 3 {
		resp, err := client.Echo(ctx, &echoRequest{Message: fmt.Sprintf("hello-%d", i)})
		if err != nil {
			lg.Error(ctx, "echo failed", "error", err)
			span.SetError(err)
			continue
		}
		lg.Info(ctx, "echo response", "response", resp.Message, "call", i+1)
	}
}
