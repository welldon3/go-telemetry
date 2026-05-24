package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	lokiFlushInterval      = time.Second
	lokiMaxBatch           = 100
	lokiQueueSize          = 1024
	lokiMaxConcurrentPush  = 2
	lokiHTTPTimeout        = 5 * time.Second
	lokiDropWarnInterval   = 100
	lokiLevelCount         = 4
	lokiMaxRetries         = 3
	lokiRetryInitialBackoff = 200 * time.Millisecond
)

// LokiConfig holds connection settings for Grafana Loki.
type LokiConfig struct {
	// Endpoint is the Loki base URL, e.g. "http://localhost:3100".
	Endpoint string

	// Labels are static stream labels added to every log stream.
	// "level" is always included automatically.
	// Set "service_name" and "service_version" here to identify the source service.
	Labels map[string]string
}

type lokiEntry struct {
	ts    time.Time
	level string
	line  string // JSON-encoded log line
}

// lokiSender batches log entries and ships them to Loki's push API.
type lokiSender struct {
	cfg     LokiConfig
	client  *http.Client
	ch      chan lokiEntry
	quit    chan struct{}
	wg      sync.WaitGroup
	dropped atomic.Int64 // total dropped entries due to full queue
	pushSem chan struct{} // limits concurrent push goroutines (max 2)
}

func newLokiSender(cfg LokiConfig) *lokiSender {
	s := &lokiSender{
		cfg:     cfg,
		client:  &http.Client{Timeout: lokiHTTPTimeout},
		ch:      make(chan lokiEntry, lokiQueueSize),
		quit:    make(chan struct{}),
		pushSem: make(chan struct{}, lokiMaxConcurrentPush),
	}
	s.wg.Add(1)
	go s.run()
	return s
}

func (s *lokiSender) enqueue(e lokiEntry) {
	select {
	case s.ch <- e:
	default:
		// Drop to never block callers; warn on first drop and every 100th after.
		n := s.dropped.Add(1)
		if n == 1 || n%lokiDropWarnInterval == 0 {
			fmt.Fprintf(os.Stderr, "go-telemetry/loki: queue full, %d log entries dropped\n", n)
		}
	}
}

func (s *lokiSender) run() {
	defer s.wg.Done()
	ticker := time.NewTicker(lokiFlushInterval)
	defer ticker.Stop()

	var batch []lokiEntry

	flush := func() {
		if len(batch) == 0 {
			return
		}
		toSend := make([]lokiEntry, len(batch))
		copy(toSend, batch)
		batch = batch[:0]
		s.pushAsync(toSend)
	}

	for {
		select {
		case e := <-s.ch:
			batch = append(batch, e)
			if len(batch) >= lokiMaxBatch {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-s.quit:
			// Drain any remaining entries before exiting.
			for {
				select {
				case e := <-s.ch:
					batch = append(batch, e)
				default:
					flush()
					return
				}
			}
		}
	}
}

// shutdown flushes buffered entries and stops the background goroutine.
func (s *lokiSender) shutdown(ctx context.Context) error {
	close(s.quit)
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type lokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][2]string       `json:"values"`
}

// pushAsync sends entries without blocking the batch loop.
// It acquires a slot from pushSem to cap concurrent goroutines at 2.
func (s *lokiSender) pushAsync(entries []lokiEntry) {
	select {
	case s.pushSem <- struct{}{}:
	default:
		fmt.Fprintf(os.Stderr, "go-telemetry/loki: too many concurrent pushes, dropping batch of %d\n", len(entries))
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() { <-s.pushSem }()
		s.push(entries)
	}()
}

func (s *lokiSender) push(entries []lokiEntry) {
	// Group by level : each level becomes a separate Loki stream.
	byLevel := make(map[string][][2]string, lokiLevelCount)
	for _, e := range entries {
		ts := fmt.Sprintf("%d", e.ts.UnixNano())
		byLevel[e.level] = append(byLevel[e.level], [2]string{ts, e.line})
	}

	streams := make([]lokiStream, 0, len(byLevel))
	for level, values := range byLevel {
		labels := make(map[string]string, len(s.cfg.Labels)+1)
		for k, v := range s.cfg.Labels {
			labels[k] = v
		}
		labels["level"] = level
		streams = append(streams, lokiStream{Stream: labels, Values: values})
	}

	body, err := json.Marshal(map[string]any{"streams": streams})
	if err != nil {
		fmt.Fprintf(os.Stderr, "go-telemetry/loki: marshal payload: %v\n", err)
		return
	}

	endpoint := strings.TrimRight(s.cfg.Endpoint, "/") + "/loki/api/v1/push"

	// Retry with exponential backoff: 0s → 200ms → 400ms (lokiMaxRetries attempts total).
	backoff := lokiRetryInitialBackoff
	for attempt := 0; attempt < lokiMaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(backoff)
			backoff *= 2
		}
		if err = s.doPush(endpoint, body); err == nil {
			return
		}
	}
	fmt.Fprintf(os.Stderr, "go-telemetry/loki: push failed after %d attempts: %v\n", lokiMaxRetries, err)
}

func (s *lokiSender) doPush(endpoint string, body []byte) error {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

// LokiExporter implements LogExporter and ships log entries to Grafana Loki.
// Create one with NewLokiExporter and pass it to Config.LogExporter.
type LokiExporter struct {
	sender *lokiSender
}

// NewLokiExporter creates a LokiExporter that ships log entries to Grafana Loki.
//
// Example:
//
//	exp := logger.NewLokiExporter(logger.LokiConfig{
//	    Endpoint: "http://localhost:3100",
//	    Labels:   map[string]string{"service_name": "my-service"},
//	})
func NewLokiExporter(cfg LokiConfig) *LokiExporter {
	if cfg.Labels == nil {
		cfg.Labels = make(map[string]string)
	}
	return &LokiExporter{sender: newLokiSender(cfg)}
}

// Export serialises the log entry and enqueues it for async delivery to Loki.
// It never blocks the caller.
func (e *LokiExporter) Export(entry LogEntry) {
	line, err := json.Marshal(entry.Attrs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "go-telemetry/loki: marshal log entry: %v\n", err)
		return
	}
	e.sender.enqueue(lokiEntry{
		ts:    entry.Time,
		level: entry.Level,
		line:  string(line),
	})
}

// Shutdown flushes buffered log entries and stops the background goroutine.
func (e *LokiExporter) Shutdown(ctx context.Context) error {
	return e.sender.shutdown(ctx)
}