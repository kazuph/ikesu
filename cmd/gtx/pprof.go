//go:build pprof

package main

import (
	"github.com/kazuph/goterrace/internal/envcompat"
	"os"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"strconv"
	"time"
)

// Profiling builds (go build -tags pprof) write a CPU profile to
// $GTX_CPUPROFILE for $GTX_CPUPROFILE_SECS seconds (default 30), so a
// benchmark can profile gtx without needing it to exit cleanly.
// $GTX_MEMPROFILE additionally dumps a heap profile to that file after
// the same delay (post-GC), which pinpoints what holds memory.
func init() {
	if tp := envcompat.Get("GTX_TRACE"); tp != "" {
		if f, err := os.Create(tp); err == nil && trace.Start(f) == nil {
			time.AfterFunc(15*time.Second, func() { trace.Stop(); f.Close() })
		}
	}
	secs, _ := strconv.Atoi(envcompat.Get("GTX_CPUPROFILE_SECS"))
	if secs <= 0 {
		secs = 30
	}
	if mp := envcompat.Get("GTX_MEMPROFILE"); mp != "" {
		time.AfterFunc(time.Duration(secs)*time.Second, func() {
			runtime.GC()
			if f, err := os.Create(mp); err == nil {
				_ = pprof.WriteHeapProfile(f)
				f.Close()
			}
		})
	}
	path := envcompat.Get("GTX_CPUPROFILE")
	if path == "" {
		return
	}
	f, err := os.Create(path)
	if err != nil {
		return
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		f.Close()
		return
	}
	time.AfterFunc(time.Duration(secs)*time.Second, func() {
		pprof.StopCPUProfile()
		f.Close()
	})
}
