package telemetrytest_test

import (
	"context"
	"testing"

	"github.com/welldon3/go-telemetry/telemetrytest"
)

func TestSpanRecorder_SpanNamed(t *testing.T) {
	rec, tr := telemetrytest.NewSpanRecorder()

	_, span := tr.Start(context.Background(), "my-op")
	span.End()

	s, ok := rec.SpanNamed("my-op")
	if !ok {
		t.Fatal("SpanNamed: span not found")
	}
	if s.Name != "my-op" {
		t.Errorf("span name: want %q, got %q", "my-op", s.Name)
	}
}

func TestSpanRecorder_SpanNamed_NotFound(t *testing.T) {
	rec, _ := telemetrytest.NewSpanRecorder()

	_, ok := rec.SpanNamed("nonexistent")
	if ok {
		t.Error("SpanNamed should return false for a span that was never started")
	}
}

func TestSpanRecorder_Reset(t *testing.T) {
	rec, tr := telemetrytest.NewSpanRecorder()

	_, span := tr.Start(context.Background(), "op")
	span.End()

	rec.Reset()

	if len(rec.Spans()) != 0 {
		t.Errorf("expected 0 spans after Reset, got %d", len(rec.Spans()))
	}
}

func TestNoopLogger_DoesNotPanic(t *testing.T) {
	log := telemetrytest.NoopLogger()
	ctx := context.Background()

	log.Debug(ctx, "debug", "k", "v")
	log.Info(ctx, "info")
	log.Warn(ctx, "warn")
	log.Error(ctx, "error")
	log.With("k", "v").Info(ctx, "with")
	log.WithGroup("g").Info(ctx, "group")
}
