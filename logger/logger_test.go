package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestLogger_InjectsTraceContext(t *testing.T) {
	var buf bytes.Buffer
	l := newWithWriter(Config{Level: LevelDebug, ServiceName: "svc", ServiceVersion: "1.0"}, &buf)

	// Create a real span so the context carries a valid trace/span ID.
	tp := sdktrace.NewTracerProvider()
	ctx, span := tp.Tracer("test").Start(context.Background(), "op")
	defer span.End()

	l.Info(ctx, "hello", "key", "val")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("log output is not valid JSON: %v\noutput: %s", err, buf.String())
	}

	for _, field := range []string{"trace_id", "span_id", "trace_flags"} {
		if _, ok := entry[field]; !ok {
			t.Errorf("expected field %q in log entry, got: %s", field, buf.String())
		}
	}

	traceID := span.SpanContext().TraceID().String()
	if got, _ := entry["trace_id"].(string); got != traceID {
		t.Errorf("trace_id: want %q, got %q", traceID, got)
	}
}

func TestLogger_NoTraceContext(t *testing.T) {
	var buf bytes.Buffer
	l := newWithWriter(Config{Level: LevelDebug}, &buf)

	l.Info(context.Background(), "msg")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("log output is not valid JSON: %v", err)
	}
	if _, ok := entry["trace_id"]; ok {
		t.Error("trace_id should be absent when context has no span")
	}
}

func TestLogger_With(t *testing.T) {
	var buf bytes.Buffer
	l := newWithWriter(Config{Level: LevelDebug}, &buf).With("env", "prod")

	l.Info(context.Background(), "msg")

	if !strings.Contains(buf.String(), `"env":"prod"`) {
		t.Errorf("expected With fields in output: %s", buf.String())
	}
}

func TestLogger_AllLevels(t *testing.T) {
	var buf bytes.Buffer
	l := newWithWriter(Config{Level: LevelDebug}, &buf)
	ctx := context.Background()

	l.Debug(ctx, "debug msg")
	l.Info(ctx, "info msg")
	l.Warn(ctx, "warn msg")
	l.Error(ctx, "error msg")

	output := buf.String()
	for _, want := range []string{"debug msg", "info msg", "warn msg", "error msg"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in output", want)
		}
	}
}
