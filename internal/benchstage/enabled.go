//go:build benchstage

package benchstage

import (
	"context"
	"golang.org/x/sys/unix"
	"runtime/trace"
	"strconv"
)

func clock() int64 {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); err != nil {
		panic(err)
	}
	return ts.Nano()
}

// Both timestamps are taken before writing the trace record. No artifact I/O
// occurs on the product PTY. Trace runs are diagnostic, never acceptance runs.
func Span(name string) func() {
	if !trace.IsEnabled() {
		return func() {}
	}
	start := clock()
	return func() {
		end := clock()
		trace.Log(context.Background(), "benchstage."+name, strconv.FormatInt(start, 10)+","+strconv.FormatInt(end, 10))
	}
}

func Values(name string, first, second int) {
	if trace.IsEnabled() {
		trace.Log(context.Background(), "benchvalue."+name, strconv.FormatInt(clock(), 10)+","+strconv.Itoa(first)+","+strconv.Itoa(second))
	}
}
