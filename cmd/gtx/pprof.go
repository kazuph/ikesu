//go:build pprof

package main

import (
	"os"
	"runtime/pprof"
	"runtime/trace"
	"strconv"
	"time"
)

// Profiling builds (go build -tags pprof) write a CPU profile to
// $GTX_CPUPROFILE for $GTX_CPUPROFILE_SECS seconds (default 30), so a
// benchmark can profile gtx without needing it to exit cleanly.
func init() {
	if tp := os.Getenv("GTX_TRACE"); tp != "" {
		if f, err := os.Create(tp); err == nil && trace.Start(f) == nil {
			time.AfterFunc(15*time.Second, func() { trace.Stop(); f.Close() })
		}
	}
	path := os.Getenv("GTX_CPUPROFILE")
	if path == "" {
		return
	}
	secs, _ := strconv.Atoi(os.Getenv("GTX_CPUPROFILE_SECS"))
	if secs <= 0 {
		secs = 30
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
