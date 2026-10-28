//go:build pprof

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"
)

// Explicitly scheduled, post-GC heap snapshots are opt-in diagnostics only.
// Ordinary builds contain neither the sampler nor its goroutine.
func init() {
	prefix := os.Getenv("IKESU_HEAP_SERIES")
	if prefix == "" {
		return
	}
	prefix = strings.ReplaceAll(prefix, "{pid}", strconv.Itoa(os.Getpid()))
	var targets []time.Duration
	for _, value := range strings.Split(os.Getenv("IKESU_HEAP_SERIES_AT"), ",") {
		at, err := time.ParseDuration(value)
		if err != nil || at <= 0 || len(targets) > 0 && at <= targets[len(targets)-1] {
			panic("IKESU_HEAP_SERIES_AT must contain increasing positive durations")
		}
		targets = append(targets, at)
	}
	start := time.Now()
	go func() {
		for _, at := range targets {
			timer := time.NewTimer(max(0, time.Until(start.Add(at))))
			<-timer.C
			runtime.GC()
			var stats runtime.MemStats
			runtime.ReadMemStats(&stats)
			profile := fmt.Sprintf("%s-%s.heap", prefix, at)
			file, err := os.Create(profile)
			if err != nil {
				panic(err)
			}
			if err = pprof.WriteHeapProfile(file); err != nil {
				file.Close()
				panic(err)
			}
			if err = file.Close(); err != nil {
				panic(err)
			}
			record := struct {
				At                                           string
				PID                                          int
				ElapsedSeconds                               float64
				HeapAlloc, HeapInuse, HeapIdle, HeapReleased uint64
				NumGC                                        uint32
				Goroutines                                   int
				Profile                                      string
			}{at.String(), os.Getpid(), time.Since(start).Seconds(), stats.HeapAlloc, stats.HeapInuse, stats.HeapIdle, stats.HeapReleased, stats.NumGC, runtime.NumGoroutine(), profile}
			data, err := json.Marshal(record)
			if err != nil {
				panic(err)
			}
			if err = os.WriteFile(fmt.Sprintf("%s-%s.json", prefix, at), append(data, '\n'), 0600); err != nil {
				panic(err)
			}
		}
	}()
}
