package main

import "golang.org/x/sys/unix"

// CLOCK_MONOTONIC is also used explicitly by the Python workload. Neither
// endpoint derives intervals from wall-clock timestamps or a process-local epoch.
func monotonicNS() int64 {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); err != nil {
		panic(err)
	}
	return ts.Nano()
}
