// Example: cli demonstrates how to instrument a short-lived batch job or CLI tool.
//
// Key difference from a long-running server: use ForceFlush before exit so
// buffered spans and metrics are exported before the process terminates.
// Without it, the SDK's internal batch buffer may not have time to flush.
//
// Run:
//
//	go run ./examples/cli
package main

import (
	"context"
	"log"
	"time"

	telemetry "github.com/welldon3/go-telemetry"
	"github.com/welldon3/go-telemetry/meter"
)

func main() {
	ctx := context.Background()

	tel, err := telemetry.New(ctx, telemetry.Config{
		ServiceName:    "import-job",
		ServiceVersion: "1.0.0",
		Environment:    "production",
		Exporter:       telemetry.ExporterStdout,
		SamplingRatio:  1.0,
	})
	if err != nil {
		log.Fatalf("telemetry init: %v", err)
	}

	// ForceFlush + Shutdown in defer — critical for CLIs and batch jobs.
	// ForceFlush drains the span/metric buffer synchronously before Shutdown releases resources.
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := tel.ForceFlush(shutCtx); err != nil {
			log.Printf("telemetry flush: %v", err)
		}
		if err := tel.Shutdown(shutCtx); err != nil {
			log.Printf("telemetry shutdown: %v", err)
		}
	}()

	logger := tel.Logger()
	tr := tel.Tracer()
	m := tel.Meter()

	if err := meter.RegisterRuntimeMetrics(m); err != nil {
		log.Fatalf("runtime metrics: %v", err)
	}

	processed, err := m.Counter("records.processed.total", "Total records imported")
	if err != nil {
		log.Fatalf("counter: %v", err)
	}

	// Custom latency histogram with production-friendly buckets.
	latency, err := m.Histogram("batch.duration", "Per-batch processing time", "ms",
		meter.WithExplicitBuckets([]float64{1, 5, 10, 25, 50, 100, 250, 500}),
	)
	if err != nil {
		log.Fatalf("histogram: %v", err)
	}

	ctx, rootSpan := tr.Start(ctx, "import-job")
	defer rootSpan.End()

	logger.Info(ctx, "import job started", "batches", 5)

	for i := range 5 {
		ctx, span := tr.Start(ctx, "process-batch")
		start := time.Now()

		// Simulate per-record work.
		time.Sleep(time.Duration(5+i*3) * time.Millisecond)
		records := 1000

		latency.Record(ctx, float64(time.Since(start).Milliseconds()))
		processed.Add(ctx, int64(records))

		logger.Info(ctx, "batch done", "batch", i+1, "records", records)
		span.SetAttribute("batch.index", i)
		span.SetAttribute("batch.records", records)
		span.End()
	}

	logger.Info(ctx, "import job complete", "total_batches", 5)
}
