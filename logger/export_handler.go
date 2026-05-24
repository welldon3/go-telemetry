package logger

import (
	"context"
	"log/slog"
	"time"
)

// exportHandler is a slog.Handler that tees records to a LogExporter
// while forwarding them to the inner handler (e.g. stdout JSON).
type exportHandler struct {
	inner    slog.Handler
	exporter LogExporter
	attrs    []slog.Attr // accumulated via WithAttrs
}

func newExportHandler(inner slog.Handler, exporter LogExporter) *exportHandler {
	return &exportHandler{inner: inner, exporter: exporter}
}

func (h *exportHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *exportHandler) Handle(ctx context.Context, r slog.Record) error {
	if err := h.inner.Handle(ctx, r); err != nil {
		return err
	}

	ts := r.Time
	if ts.IsZero() {
		ts = time.Now()
	}

	m := make(map[string]any, r.NumAttrs()+len(h.attrs)+1)
	m["msg"] = r.Message
	for _, a := range h.attrs {
		m[a.Key] = a.Value.Any()
	}
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value.Any()
		return true
	})

	h.exporter.Export(LogEntry{
		Time:    ts,
		Level:   r.Level.String(),
		Message: r.Message,
		Attrs:   m,
	})
	return nil
}

func (h *exportHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	merged := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(merged, h.attrs)
	copy(merged[len(h.attrs):], attrs)
	return &exportHandler{
		inner:    h.inner.WithAttrs(attrs),
		exporter: h.exporter,
		attrs:    merged,
	}
}

func (h *exportHandler) WithGroup(name string) slog.Handler {
	return &exportHandler{
		inner:    h.inner.WithGroup(name),
		exporter: h.exporter,
		attrs:    h.attrs,
	}
}