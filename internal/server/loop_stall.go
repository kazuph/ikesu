package server

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/tui"
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
	dir := filepath.Join(s.dir, "app-loop-stalls")
	if err := os.MkdirAll(dir, 0700); err != nil {
		fmt.Fprintln(os.Stderr, "ikesu stall diagnostics:", err)
		return
	}
	path := filepath.Join(dir, now.UTC().Format("20060102T150405.000000000Z")+".stack")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ikesu stall diagnostics:", err)
		return
	}
	err = writeStacks(f)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		fmt.Fprintln(os.Stderr, "ikesu stall diagnostics:", err)
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ikesu stall diagnostics:", err)
		return
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".stack" {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for len(names) > stallStackLimit {
		if err := os.Remove(filepath.Join(dir, names[0])); err != nil {
			fmt.Fprintln(os.Stderr, "ikesu stall diagnostics:", err)
			break
		}
		names = names[1:]
	}
}
