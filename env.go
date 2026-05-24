package telemetry

import (
	"os"
	"strconv"
	"strings"

	"github.com/welldon3/go-telemetry/logger"
)

// ConfigFromEnv returns a copy of cfg with values overridden by standard
// OTEL_* environment variables. Call this before passing Config to New():
//
//	cfg := telemetry.ConfigFromEnv(telemetry.Config{
//	    Exporter: telemetry.ExporterOTLP,
//	})
//	tel, err := telemetry.New(ctx, cfg)
//
// Supported variables:
//
//	OTEL_SERVICE_NAME            : overrides Config.ServiceName
//	OTEL_SERVICE_VERSION         : overrides Config.ServiceVersion
//	OTEL_TRACES_EXPORTER         : otlp|stdout|prometheus|none
//	OTEL_EXPORTER_OTLP_ENDPOINT  : overrides Config.OTLPEndpoint
//	OTEL_EXPORTER_OTLP_INSECURE  : "true" sets Config.OTLPInsecure
//	OTEL_TRACES_SAMPLER          : always_on|always_off|traceidratio
//	OTEL_TRACES_SAMPLER_ARG      : float64, used when sampler is traceidratio
//	OTEL_RESOURCE_ATTRIBUTES     : comma-separated key=value pairs
//	OTEL_LOG_LEVEL               : debug|info|warn|error
func ConfigFromEnv(cfg Config) Config {
	if v := os.Getenv("OTEL_SERVICE_NAME"); v != "" {
		cfg.ServiceName = v
	}
	if v := os.Getenv("OTEL_SERVICE_VERSION"); v != "" {
		cfg.ServiceVersion = v
	}
	if v := os.Getenv("OTEL_TRACES_EXPORTER"); v != "" {
		var exporters []ExporterType
		for _, part := range strings.Split(v, ",") {
			switch strings.ToLower(strings.TrimSpace(part)) {
			case "otlp":
				exporters = append(exporters, ExporterOTLP)
			case "prometheus":
				exporters = append(exporters, ExporterPrometheus)
			case "none":
				exporters = append(exporters, ExporterNoop)
			case "stdout":
				exporters = append(exporters, ExporterStdout)
			}
		}
		if len(exporters) == 1 {
			cfg.Exporter = exporters[0]
		} else if len(exporters) > 1 {
			cfg.Exporters = exporters
		}
	}
	if v := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); v != "" {
		cfg.OTLPEndpoint = v
	}
	if v := os.Getenv("OTEL_EXPORTER_OTLP_INSECURE"); strings.EqualFold(v, "true") {
		cfg.OTLPInsecure = true
	}
	if v := os.Getenv("OTEL_TRACES_SAMPLER"); v != "" {
		switch strings.ToLower(v) {
		case "always_on", "parentbased_always_on":
			cfg.SamplingRatio = 1.0
		case "always_off", "parentbased_always_off":
			cfg.SamplingRatio = 0.0
		case "traceidratio", "parentbased_traceidratio":
			// ratio is read from OTEL_TRACES_SAMPLER_ARG below
		}
	}
	if v := os.Getenv("OTEL_TRACES_SAMPLER_ARG"); v != "" {
		if ratio, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.SamplingRatio = ratio
		}
	}
	if v := os.Getenv("OTEL_RESOURCE_ATTRIBUTES"); v != "" {
		if cfg.ResourceAttributes == nil {
			cfg.ResourceAttributes = make(map[string]string)
		}
		for _, pair := range strings.Split(v, ",") {
			k, val, ok := strings.Cut(strings.TrimSpace(pair), "=")
			if ok && k != "" {
				cfg.ResourceAttributes[k] = val
			}
		}
	}
	if v := os.Getenv("OTEL_LOG_LEVEL"); v != "" {
		switch strings.ToLower(v) {
		case "debug":
			cfg.LogLevel = logger.LevelDebug
		case "warn":
			cfg.LogLevel = logger.LevelWarn
		case "error":
			cfg.LogLevel = logger.LevelError
		default:
			cfg.LogLevel = logger.LevelInfo
		}
	}
	return cfg
}