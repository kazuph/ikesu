//go:build pprof

package main

import (
	"os"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"strconv"
	"time"
)

// Profiling builds (go build -tags pprof) write a CPU profile to
// $IKESU_CPUPROFILE for $IKESU_CPUPROFILE_SECS seconds (default 30), so a
// benchmark can profile ikesu without needing it to exit cleanly.
// $IKESU_MEMPROFILE additionally dumps a heap profile to that file after
// the same delay (post-GC), which pinpoints what holds memory.
func init() {
	if tp := os.Getenv("IKESU_TRACE"); tp != "" {
		if f, err := os.Create(tp); err == nil && trace.Start(f) == nil {
			if stop := os.Getenv("IKESU_TRACE_STOP_FILE"); stop != "" {
				go func() {
					tick := time.NewTicker(100 * time.Millisecond)
					defer tick.Stop()
					for range tick.C {
						if _, err := os.Stat(stop); err == nil {
							trace.Stop()
							f.Close()
							_ = os.WriteFile(stop+".done", []byte("closed\n"), 0600)
							return
						}
					}
				}()
			} else {
				time.AfterFunc(15*time.Second, func() { trace.Stop(); f.Close() })
			}
		}
	}
	secs, _ := strconv.Atoi(os.Getenv("IKESU_CPUPROFILE_SECS"))
	if secs <= 0 {
		secs = 30
	}
	if mp := os.Getenv("IKESU_MEMPROFILE"); mp != "" {
		time.AfterFunc(time.Duration(secs)*time.Second, func() {
			runtime.GC()
			if f, err := os.Create(mp); err == nil {
				_ = pprof.WriteHeapProfile(f)
				f.Close()
			}
		})
	}
	path := os.Getenv("IKESU_CPUPROFILE")
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
