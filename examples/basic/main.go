// Example: basic demonstrates a fully instrumented HTTP server using go-telemetry.
//
// Run with the local Grafana stack (from the repo root):
//
//	docker-compose up -d
//	go run ./examples/basic
//
// Then open Grafana at http://localhost:3000 (admin/admin).
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	telemetry "github.com/welldon3/go-telemetry"
	"github.com/welldon3/go-telemetry/logger"
	"github.com/welldon3/go-telemetry/meter"
	"github.com/welldon3/go-telemetry/middleware"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Config can be overridden via env vars:
	//   OTEL_SERVICE_NAME, OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_TRACES_SAMPLER_ARG, etc.
	cfg := telemetry.ConfigFromEnv(telemetry.Config{
		ServiceName:        "example-service",
		ServiceVersion:     "0.1.0",
		Environment:        "development",
		Exporter:           telemetry.ExporterOTLP,
		OTLPEndpoint:       "localhost:4317",
		OTLPInsecure:       true,                      // local Tempo, no TLS needed
		MetricExporter:     telemetry.ExporterPrometheus, // Prometheus scrapes :9091/metrics
		PrometheusPort:     9091,                      // 9090 is taken by the Prometheus container
		LogExporter: logger.NewLokiExporter(logger.LokiConfig{
			Endpoint: "http://localhost:3100",
			Labels: map[string]string{
				"service_name":    "example-service",
				"service_version": "0.1.0",
			},
		}),
		SamplingRatio:      1.0,
		AlwaysSampleErrors: true,
	})

	tel, err := telemetry.New(ctx, cfg)
	if err != nil {
		log.Fatalf("telemetry init: %v", err)
	}
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := tel.Shutdown(shutCtx); err != nil {
			log.Printf("telemetry shutdown: %v", err)
		}
	}()

	logger := tel.Logger()
	tr := tel.Tracer()
	m := tel.Meter()

	// Register runtime metrics: goroutines, heap, GC pause.
	if err := meter.RegisterRuntimeMetrics(m); err != nil {
		log.Fatalf("runtime metrics: %v", err)
	}

	// Application counter.
	processedCounter, err := m.Counter("orders.processed.total", "Total orders processed")
	if err != nil {
		log.Fatalf("counter: %v", err)
	}

	// Business logic handler.
	processOrder := func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tr.Start(r.Context(), "processOrder")
		defer span.End()

		time.Sleep(5 * time.Millisecond)

		orderID := r.URL.Query().Get("id")
		if orderID == "" {
			err := errors.New("missing order id")
			span.SetError(err)
			// trace_id is injected automatically — no extra wiring needed.
			logger.Error(ctx, "order validation failed", "error", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		span.SetAttribute("order.id", orderID)
		processedCounter.Inc(ctx)
		logger.Info(ctx, "order processed", "order_id", orderID)
		w.WriteHeader(http.StatusOK)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/orders", processOrder)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	instrumented := middleware.HTTP(middleware.HTTPConfig{
		Tracer: tr,
		Logger: logger,
		Meter:  m,
	})(mux)

	srv := &http.Server{Addr: ":8080", Handler: instrumented}

	go func() {
		logger.Info(ctx, "server starting", "addr", ":8080",
			"grafana", "http://localhost:3000",
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutCtx)
}