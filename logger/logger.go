// Package logger provides structured logging with automatic trace context injection.
package logger

import (
	"context"
	"io"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel/trace"
)

// Level maps to slog levels.
type Level = slog.Level

const (
	LevelDebug = slog.LevelDebug
	LevelInfo  = slog.LevelInfo
	LevelWarn  = slog.LevelWarn
	LevelError = slog.LevelError
)

// Logger is the structured logging interface.
// Every call injects trace_id and span_id from ctx into the log entry.
type Logger interface {
	Debug(ctx context.Context, msg string, args ...any)
	Info(ctx context.Context, msg string, args ...any)
	Warn(ctx context.Context, msg string, args ...any)
	Error(ctx context.Context, msg string, args ...any)

	With(args ...any) Logger
	WithGroup(name string) Logger
}

// Config holds logger construction options.
type Config struct {
	Level          Level
	ServiceName    string
	ServiceVersion string
}

type slogLogger struct {
	inner *slog.Logger
}

// New creates a Logger backed by slog writing JSON to stdout.
func New(cfg Config) Logger {
	return newWithWriter(cfg, os.Stdout)
}

// newWithWriter is used in tests to capture log output.
func newWithWriter(cfg Config, w io.Writer) Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: cfg.Level,
	})
	base := slog.New(h).With(
		"service.name", cfg.ServiceName,
		"service.version", cfg.ServiceVersion,
	)
	return &slogLogger{inner: base}
}

// NewWithExporter creates a Logger that writes JSON to w and also forwards
// every entry to exp. The returned function must be called on shutdown.
func NewWithExporter(cfg Config, w io.Writer, exp LogExporter) (Logger, func(context.Context) error) {
	base := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: cfg.Level})
	combined := newExportHandler(base, exp)
	l := slog.New(combined).With(
		"service.name", cfg.ServiceName,
		"service.version", cfg.ServiceVersion,
	)
	return &slogLogger{inner: l}, exp.Shutdown
}

func traceAttrs(ctx context.Context) []any {
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return nil
	}
	sc := span.SpanContext()
	return []any{
		"trace_id", sc.TraceID().String(),
		"span_id", sc.SpanID().String(),
		"trace_flags", sc.TraceFlags().String(),
	}
}

func (l *slogLogger) Debug(ctx context.Context, msg string, args ...any) {
	l.inner.DebugContext(ctx, msg, append(traceAttrs(ctx), args...)...)
}

func (l *slogLogger) Info(ctx context.Context, msg string, args ...any) {
	l.inner.InfoContext(ctx, msg, append(traceAttrs(ctx), args...)...)
}

func (l *slogLogger) Warn(ctx context.Context, msg string, args ...any) {
	l.inner.WarnContext(ctx, msg, append(traceAttrs(ctx), args...)...)
}

func (l *slogLogger) Error(ctx context.Context, msg string, args ...any) {
	l.inner.ErrorContext(ctx, msg, append(traceAttrs(ctx), args...)...)
}

func (l *slogLogger) With(args ...any) Logger {
	return &slogLogger{inner: l.inner.With(args...)}
}

func (l *slogLogger) WithGroup(name string) Logger {
	return &slogLogger{inner: l.inner.WithGroup(name)}
}
