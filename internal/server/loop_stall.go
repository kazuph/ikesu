package server

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/tui"
	"golang.org/x/sys/unix"
)

const stallThreshold = tui.LoopStallThreshold
const stallStackInterval = time.Minute
const stallStackLimit = 20

type stallMonitor struct{ lastStack time.Time }

func (s *Server) stallStatus() api.LoopStallStatus {
	heartbeat, count, longest := s.app.LoopTiming()
	var current time.Duration
	if !heartbeat.IsZero() && heartbeat.UnixNano() != 0 {
		elapsed := time.Since(heartbeat)
		if elapsed >= stallThreshold {
			current = elapsed
			count++
			if current > longest {
				longest = current
			}
		}
	}
	result := api.LoopStallStatus{Count: count, LongestMillis: longest.Milliseconds(), CurrentMillis: current.Milliseconds()}
	if current > 0 && s.sup != nil {
		ids, known := s.sup.LockedPanes()
		result.PaneLocksKnown = known
		for _, id := range ids {
			result.LockedPanes = append(result.LockedPanes, fmtPaneID(id))
		}
	}
	return result
}

func (s *Server) watchAppLoop(done <-chan struct{}) {
	for {
		heartbeat, _, _ := s.app.LoopTiming()
		now := time.Now()
		next := now.Add(tui.LoopHeartbeatInterval)
		if !heartbeat.IsZero() && heartbeat.UnixNano() != 0 {
			next = heartbeat.Add(stallThreshold)
			if limit := s.stall.lastStack.Add(stallStackInterval); !s.stall.lastStack.IsZero() && next.Before(limit) {
				next = limit
			}
		}
		timer := time.NewTimer(max(0, time.Until(next)))
		select {
		case <-done:
			timer.Stop()
			return
		case <-timer.C:
		}
		now = time.Now()
		heartbeat, _, _ = s.app.LoopTiming()
		if heartbeat.IsZero() || heartbeat.UnixNano() == 0 || now.Sub(heartbeat) < stallThreshold {
			continue
		}
		if !s.stall.lastStack.IsZero() && now.Sub(s.stall.lastStack) < stallStackInterval {
			continue
		}
		s.stall.lastStack = now
		s.saveStallStack(now)
	}
}

func (s *Server) saveStallStack(now time.Time) {
	root, err := unix.Open(s.dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ikesu stall diagnostics:", err)
		return
	}
	defer unix.Close(root)
	if err := unix.Mkdirat(root, "app-loop-stalls", 0700); err != nil && err != unix.EEXIST {
		fmt.Fprintln(os.Stderr, "ikesu stall diagnostics:", err)
		return
	}
	fd, err := unix.Openat(root, "app-loop-stalls", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ikesu stall diagnostics:", err)
		return
	}
	dir := os.NewFile(uintptr(fd), "app-loop-stalls")
	defer dir.Close()
	name := now.UTC().Format("20060102T150405.000000000Z") + ".stack"
	fileFD, err := unix.Openat(fd, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ikesu stall diagnostics:", err)
		return
	}
	f := os.NewFile(uintptr(fileFD), name)
	err = writeStacks(f)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = unix.Unlinkat(fd, name, 0)
		fmt.Fprintln(os.Stderr, "ikesu stall diagnostics:", err)
		return
	}
	entries, err := dir.ReadDir(-1)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ikesu stall diagnostics:", err)
		return
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		const format = "20060102T150405.000000000Z"
		if len(name) != len(format)+len(".stack") || name[len(format):] != ".stack" || !entry.Type().IsRegular() {
			continue
		}
		stamp, err := time.Parse(format, name[:len(format)])
		if err == nil && stamp.UTC().Format(format) == name[:len(format)] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for len(names) > stallStackLimit {
		if err := unix.Unlinkat(fd, names[0], 0); err != nil {
			fmt.Fprintln(os.Stderr, "ikesu stall diagnostics:", err)
			break
		}
		names = names[1:]
	}
}
