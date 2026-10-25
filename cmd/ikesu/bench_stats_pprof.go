//go:build pprof

package main

import (
	"encoding/json"
	paneruntime "github.com/kazuph/ikesu/internal/runtime"
	"math"
	"os"
	"runtime/metrics"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// Opt-in measurement only: ordinary binaries contain no sampler. Reading
// runtime/metrics avoids forcing a GC or the stop-the-world ReadMemStats call.
func init() {
	path := os.Getenv("IKESU_BENCH_STATS")
	if path == "" {
		return
	}
	path = strings.ReplaceAll(path, "{pid}", strconv.Itoa(os.Getpid()))
	file, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	go func() {
		defer file.Close()
		encoder := json.NewEncoder(file)
		names := []string{"/gc/heap/allocs:bytes", "/gc/heap/frees:bytes", "/gc/heap/live:bytes", "/memory/classes/heap/objects:bytes", "/memory/classes/total:bytes", "/gc/pauses:seconds", "/gc/cycles/total:gc-cycles"}
		samples := make([]metrics.Sample, len(names))
		for i, name := range names {
			samples[i].Name = name
		}
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for range tick.C {
			metrics.Read(samples)
			var ts unix.Timespec
			if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); err != nil {
				return
			}
			record := map[string]any{"clock_ns": ts.Nano(), "pid": os.Getpid(), "panes": paneruntime.BenchPaneStats()}
			for _, sample := range samples {
				switch sample.Value.Kind() {
				case metrics.KindUint64:
					record[sample.Name] = sample.Value.Uint64()
				case metrics.KindFloat64Histogram:
					histogram := sample.Value.Float64Histogram()
					// Infinite boundary values cannot be JSON numbers. Preserve them as
					// strings; finite bucket boundaries keep their original precision.
					bounds := make([]any, len(histogram.Buckets))
					for i, v := range histogram.Buckets {
						if math.IsInf(v, -1) {
							bounds[i] = "-Inf"
						} else if math.IsInf(v, 1) {
							bounds[i] = "+Inf"
						} else {
							bounds[i] = v
						}
					}
					record[sample.Name] = map[string]any{"buckets": bounds, "counts": histogram.Counts}
				}
			}
			if err := encoder.Encode(record); err != nil {
				return
			}
		}
	}()
}
