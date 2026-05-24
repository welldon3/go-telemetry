package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// lokiServer captures the last push payload for assertions.
type lokiServer struct {
	*httptest.Server
	received chan []byte
}

func newLokiServer(t *testing.T) *lokiServer {
	t.Helper()
	ls := &lokiServer{received: make(chan []byte, 1)}
	ls.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		select {
		case ls.received <- body:
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(ls.Close)
	return ls
}

// waitForPush blocks until Loki receives a push or the test times out.
func (ls *lokiServer) waitForPush(t *testing.T) []byte {
	t.Helper()
	select {
	case body := <-ls.received:
		return body
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Loki push")
		return nil
	}
}

func TestLokiExporter_ShipsLogs(t *testing.T) {
	srv := newLokiServer(t)

	exp := NewLokiExporter(LokiConfig{
		Endpoint: srv.URL,
		Labels:   map[string]string{"service_name": "test-svc", "service_version": "2.0"},
	})

	var buf bytes.Buffer
	l, shutdown := NewWithExporter(
		Config{Level: LevelInfo, ServiceName: "test-svc", ServiceVersion: "2.0"},
		&buf,
		exp,
	)

	l.Info(context.Background(), "hello loki", "key", "val")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	body := srv.waitForPush(t)

	var payload struct {
		Streams []struct {
			Stream map[string]string `json:"stream"`
			Values [][2]string       `json:"values"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("invalid Loki payload JSON: %v\nbody: %s", err, body)
	}
	if len(payload.Streams) == 0 {
		t.Fatal("expected at least one stream in Loki payload")
	}

	stream := payload.Streams[0]
	if stream.Stream["service_name"] != "test-svc" {
		t.Errorf("stream label service_name: want %q, got %q", "test-svc", stream.Stream["service_name"])
	}
	if stream.Stream["service_version"] != "2.0" {
		t.Errorf("stream label service_version: want %q, got %q", "2.0", stream.Stream["service_version"])
	}
	if _, ok := stream.Stream["level"]; !ok {
		t.Error("stream label 'level' must be present")
	}
	if len(stream.Values) == 0 {
		t.Fatal("expected at least one log entry value")
	}

	var logLine map[string]any
	if err := json.Unmarshal([]byte(stream.Values[0][1]), &logLine); err != nil {
		t.Fatalf("log line is not valid JSON: %v\nline: %s", err, stream.Values[0][1])
	}
	if logLine["msg"] != "hello loki" {
		t.Errorf("log line msg: want %q, got %v", "hello loki", logLine["msg"])
	}
	if logLine["key"] != "val" {
		t.Errorf("log line key: want %q, got %v", "val", logLine["key"])
	}
}

func TestLokiExporter_StillWritesToWriter(t *testing.T) {
	srv := newLokiServer(t)

	exp := NewLokiExporter(LokiConfig{Endpoint: srv.URL})

	var buf bytes.Buffer
	l, shutdown := NewWithExporter(
		Config{Level: LevelInfo, ServiceName: "svc"},
		&buf,
		exp,
	)

	l.Info(context.Background(), "also in stdout")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = shutdown(ctx)

	if !strings.Contains(buf.String(), "also in stdout") {
		t.Errorf("expected log in writer output, got: %s", buf.String())
	}
}

func TestLokiExporter_WithFieldsInLokiLine(t *testing.T) {
	srv := newLokiServer(t)

	exp := NewLokiExporter(LokiConfig{Endpoint: srv.URL})

	var buf bytes.Buffer
	l, shutdown := NewWithExporter(
		Config{Level: LevelDebug, ServiceName: "svc"},
		&buf,
		exp,
	)

	// Fields added via With() must appear in the Loki log line.
	l.With("env", "prod").Info(context.Background(), "msg")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = shutdown(ctx)

	body := srv.waitForPush(t)

	var payload struct {
		Streams []struct {
			Values [][2]string `json:"values"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || len(payload.Streams) == 0 {
		t.Fatalf("bad payload: %v — %s", err, body)
	}

	var logLine map[string]any
	_ = json.Unmarshal([]byte(payload.Streams[0].Values[0][1]), &logLine)
	if logLine["env"] != "prod" {
		t.Errorf("With() field 'env' missing from Loki log line: %v", logLine)
	}
}

func TestLokiExporter_BatchesByLevel(t *testing.T) {
	received := make(chan []byte, 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- body
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	exp := NewLokiExporter(LokiConfig{Endpoint: srv.URL})

	var buf bytes.Buffer
	l, shutdown := NewWithExporter(
		Config{Level: LevelDebug, ServiceName: "svc"},
		&buf,
		exp,
	)

	ctx := context.Background()
	l.Info(ctx, "info msg")
	l.Error(ctx, "error msg")

	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = shutdown(shutCtx)

	// Collect all received payloads and verify both levels appear.
	levels := make(map[string]bool)
	for {
		select {
		case body := <-received:
			var payload struct {
				Streams []struct {
					Stream map[string]string `json:"stream"`
				} `json:"streams"`
			}
			_ = json.Unmarshal(body, &payload)
			for _, s := range payload.Streams {
				levels[s.Stream["level"]] = true
			}
		default:
			if !levels["INFO"] {
				t.Error("expected INFO level stream in Loki payload")
			}
			if !levels["ERROR"] {
				t.Error("expected ERROR level stream in Loki payload")
			}
			return
		}
	}
}