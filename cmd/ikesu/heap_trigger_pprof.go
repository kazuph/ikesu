//go:build pprof && (darwin || linux)

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// A diagnostic build can take a post-GC snapshot when its measured server is signalled.
func init() {
	prefix := os.Getenv("IKESU_HEAP_TRIGGER_PREFIX")
	if prefix == "" {
		return
	}
	prefix = strings.ReplaceAll(prefix, "{pid}", fmt.Sprint(os.Getpid()))
	requests := make(chan os.Signal, 1)
	signal.Notify(requests, syscall.SIGUSR2)
	go func() {
		for sequence := 1; ; sequence++ {
			<-requests
			if err := writeTriggeredHeap(prefix, sequence); err != nil {
				_ = os.WriteFile(fmt.Sprintf("%s-%d.error", prefix, sequence), []byte(err.Error()), 0600)
			}
		}
	}()
}

func writeTriggeredHeap(prefix string, sequence int) error {
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	var stamp unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &stamp); err != nil {
		return err
	}
	base := fmt.Sprintf("%s-%d", prefix, sequence)
	profile := base + ".heap"
	file, err := os.Create(profile)
	if err != nil {
		return err
	}
	err = pprof.WriteHeapProfile(file)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	record := struct {
		PID          int    `json:"pid"`
		MonotonicNS  int64  `json:"monotonic_ns"`
		HeapAlloc    uint64 `json:"heap_alloc_bytes"`
		HeapInuse    uint64 `json:"heap_inuse_bytes"`
		HeapIdle     uint64 `json:"heap_idle_bytes"`
		HeapReleased uint64 `json:"heap_released_bytes"`
		Profile      string `json:"profile"`
	}{os.Getpid(), stamp.Nano(), stats.HeapAlloc, stats.HeapInuse, stats.HeapIdle, stats.HeapReleased, profile}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	tmp := base + ".json.tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0600); err != nil {
		return err
	}
	return os.Rename(tmp, base+".json")
}
