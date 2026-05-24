package logger

import (
	"context"
	"time"
)

// LogEntry is a single log record delivered to a LogExporter.
type LogEntry struct {
	Time    time.Time
	Level   string
	Message string
	// Attrs contains all key-value pairs from the log record (including "msg").
	Attrs map[string]any
}

// LogExporter ships log entries to an external backend (e.g. Loki, Elasticsearch).
// Implementations must be safe for concurrent use.
type LogExporter interface {
	// Export delivers a single log entry. Implementations should not block
	// the caller; entries may be queued internally.
	Export(entry LogEntry)
	// Shutdown flushes any buffered entries and releases resources.
	Shutdown(ctx context.Context) error
}