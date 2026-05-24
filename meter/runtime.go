package meter

import (
	"runtime/metrics"
)

// RuntimeMetrics registers observable gauges that report Go runtime
// statistics on every metrics collection cycle.
// Uses runtime/metrics (Go 1.16+) : no stop-the-world pauses.
// Call this once after creating a Meter.
func RegisterRuntimeMetrics(m Meter) error {
	if err := m.Gauge(
		"go.goroutines",
		"Number of goroutines that currently exist",
		func() float64 { return readUint64("/sched/goroutines:goroutines") },
	); err != nil {
		return err
	}

	if err := m.Gauge(
		"go.heap_alloc_bytes",
		"Bytes of allocated heap objects",
		func() float64 { return readUint64("/memory/classes/heap/objects:bytes") },
	); err != nil {
		return err
	}

	if err := m.Gauge(
		"go.gc_pause_ns",
		"Approximate total nanoseconds spent in GC pauses (sum of pause histogram)",
		func() float64 {
			s := []metrics.Sample{{Name: "/gc/pauses:seconds"}}
			metrics.Read(s)
			if s[0].Value.Kind() != metrics.KindFloat64Histogram {
				return 0
			}
			h := s[0].Value.Float64Histogram()
			var total float64
			for i, count := range h.Counts {
				if count == 0 || i+1 >= len(h.Buckets) {
					continue
				}
				mid := (h.Buckets[i] + h.Buckets[i+1]) / 2
				total += float64(count) * mid
			}
			return total * 1e9 // seconds → nanoseconds
		},
	); err != nil {
		return err
	}

	if err := m.Gauge(
		"go.heap_objects",
		"Number of live heap objects",
		func() float64 { return readUint64("/gc/heap/objects:objects") },
	); err != nil {
		return err
	}

	return nil
}

// readUint64 reads a single uint64 runtime metric without any STW pause.
func readUint64(name string) float64 {
	s := []metrics.Sample{{Name: name}}
	metrics.Read(s)
	if s[0].Value.Kind() == metrics.KindUint64 {
		return float64(s[0].Value.Uint64())
	}
	return 0
}