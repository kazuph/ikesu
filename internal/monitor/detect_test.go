package monitor

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/config"
)

func TestManagedTree(t *testing.T) {
	t.Parallel()
	rows, err := parseSample([]byte("10 1 50 1:05.40\n11 10 70 00:00:00\n12 11 80 00:00:01\n13 11 90 1-00:00:00\n20 1 100000 00:00:00\n22 10 20 0:00.00\n"))
	if err != nil {
		t.Fatal(err)
	}
	roots := Roots{1: {10, "a"}, 2: {12, "b"}}
	read := func(row Process) (Process, error) {
		row.Start = "a"
		if row.PID == 12 {
			row.Start = "b"
		}
		if row.PID == 22 {
			row.Parent = 20
		}
		return row, nil
	}
	got := map[int]int{}
	for _, row := range managed(rows, roots, read) {
		got[row.PID] = row.Pane
	}
	if len(got) != 4 || got[10] != 1 || got[11] != 1 || got[12] != 2 || got[13] != 1 {
		t.Fatalf("managed trees = %v", got)
	}
	if rows[0].CPU != 65*time.Second+400*time.Millisecond || rows[3].CPU != 24*time.Hour {
		t.Fatalf("parsed CPU time: %v / %v", rows[0].CPU, rows[3].CPU)
	}
}

func TestDetectRunawayThresholds(t *testing.T) {
	t.Parallel()
	start := time.Unix(100, 0)
	cfg := config.RunawaySettings{Interval: 5 * time.Second, MemoryKiB: 100, SpikeKiB: 50, CPUPercent: 180, CPUDuration: 10 * time.Second}
	root, id := Identity{10, "root"}, Identity{20, "child"}
	roots := Roots{1: root}
	proc := func(rss int64, cpu time.Duration) Process { return Process{Identity: id, Pane: 1, RSS: rss, CPU: cpu} }
	t.Run("rss and tree total", func(t *testing.T) {
		var d Detector
		rows := []Process{{Identity: root, Pane: 1, RSS: 40}, proc(60, 0)}
		if got := d.Observe(start, cfg, roots, rows); got[1].Breach {
			t.Fatalf("exact limit breached: %+v", got)
		}
		rows[0].RSS++
		if got := d.Observe(start.Add(5*time.Second), cfg, roots, rows); !got[1].Breach || got[0].Breach || got[1].TreeRSS != 101 {
			t.Fatalf("tree breach: %+v", got)
		}
		if got := d.Observe(start.Add(10*time.Second), cfg, roots, []Process{proc(101, 0)}); !got[0].Breach {
			t.Fatalf("process RSS: %+v", got)
		}
	})
	t.Run("rolling 30 second spike and PID reuse", func(t *testing.T) {
		var d Detector
		d.Observe(start, cfg, roots, []Process{proc(10, 0)})
		d.Observe(start.Add(20*time.Second), cfg, roots, []Process{proc(40, 0)})
		if got := d.Observe(start.Add(30*time.Second), cfg, roots, []Process{proc(60, 0)}); !got[0].Breach || got[0].Spike != 50 {
			t.Fatalf("inclusive spike: %+v", got)
		}
		if got := d.Observe(start.Add(31*time.Second), cfg, roots, []Process{proc(60, 0)}); got[0].Breach || got[0].Spike != 20 {
			t.Fatalf("expired baseline: %+v", got)
		}
		reused := proc(95, 0)
		reused.Start = "new start"
		if got := d.Observe(start.Add(32*time.Second), cfg, roots, []Process{reused}); got[0].Spike != 0 {
			t.Fatalf("inherited process history: %+v", got)
		}
	})
	t.Run("sustained CPU with quiet and missed samples", func(t *testing.T) {
		var d Detector
		for i, cpu := range []int{0, 9, 18, 19, 28, 37} {
			now := start.Add(time.Duration(i*5) * time.Second)
			got := d.Observe(now, cfg, roots, []Process{proc(10, time.Duration(cpu)*time.Second)})[0]
			if got.Breach != (i == 2 || i == 5) {
				t.Fatalf("sample %d: %+v", i, got)
			}
		}
		if got := d.Observe(start.Add(time.Minute), cfg, roots, []Process{proc(10, 2*time.Minute)}); got[0].HighCPU != 0 || got[0].Breach {
			t.Fatalf("credited missed samples: %+v", got)
		}
		d.Observe(start.Add(65*time.Second), cfg, roots, nil)
		if len(d.processes) != 0 || len(d.trees) != 0 {
			t.Fatal("stale process history survived exit")
		}
	})
}

func TestValidateOwnerRejectsOtherPaneAndReusedPID(t *testing.T) {
	t.Parallel()
	target, root, other := Identity{22, "target"}, Identity{10, "root"}, Identity{11, "other"}
	parents := map[int]int{22: 21, 21: 10, 10: 1, 11: 10}
	starts := map[int]string{22: target.Start, 21: "worker", 10: root.Start, 11: other.Start}
	roots := Roots{1: root, 2: other}
	read := func(pid int) (Process, error) {
		if s, ok := starts[pid]; ok {
			return Process{Identity: Identity{pid, s}, Parent: parents[pid]}, nil
		}
		return Process{}, os.ErrNotExist
	}
	if err := validateOwner(target, root, roots, read); err != nil {
		t.Fatalf("managed descendant rejected: %v", err)
	}
	for _, tc := range []struct {
		label  string
		parent int
		start  string
		owner  Identity
	}{
		{"nested pane", 11, target.Start, root},
		{"unrelated", 1, target.Start, root},
		{"reused PID", 21, "replacement", root},
		{"root identity changed", 21, target.Start, Identity{10, "old"}},
	} {
		parents[22], starts[22] = tc.parent, tc.start
		if err := validateOwner(target, tc.owner, roots, read); err == nil {
			t.Errorf("accepted %s", tc.label)
		}
	}
}

func TestRealProcessSample(t *testing.T) {
	t.Parallel()
	pid := os.Getpid()
	proc, err := processInfo(pid)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rows, err := Sample(ctx, Roots{1: proc.Identity})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.PID == pid && row.Start == proc.Start && row.RSS > 0 {
			return
		}
	}
	t.Fatalf("test process missing from sample: %+v", rows)
}

func TestRunawayReusedPIDCannotInheritMeasurements(t *testing.T) {
	t.Parallel()
	root, first, next := Identity{10, "shell"}, Identity{20, "first"}, Identity{20, "replacement"}
	rows := []Process{{Identity: root, Parent: 1, RSS: 10}, {Identity: first, Parent: 10, RSS: 10000, CPU: time.Hour}}
	fresh := map[int]Process{10: rows[0], 20: {Identity: next, Parent: 10, RSS: 4, CPU: time.Second}}
	native := func(pid int) (Process, error) { return fresh[pid], nil }
	old := map[int]Process{10: rows[0], 20: rows[1]}
	roots := Roots{1: root}
	for _, read := range []readSample{bracketSample(old, native), func(row Process) (Process, error) { return native(row.PID) }} {
		managedRows := managed(rows, roots, read)
		var d Detector
		cfg := config.RunawaySettings{Interval: time.Second, MemoryKiB: 1000, SpikeKiB: 1000, CPUPercent: 180, CPUDuration: time.Minute}
		for _, m := range d.Observe(time.Now(), cfg, roots, managedRows) {
			if m.Breach || m.Identity == next && (m.RSS != 4 || m.CPU != time.Second) {
				t.Fatalf("replacement inherited a previous process sample: %+v", m)
			}
		}
	}
}

func TestRunawaySpikeWithSlowPollingAndSamplingDelay(t *testing.T) {
	t.Parallel()
	cfg := config.Config{Runaway: config.RunawayConfig{PollSeconds: 60}}.RunawaySettings()
	if cfg.Interval != 15*time.Second {
		t.Fatalf("60-second setting resolved to %s", cfg.Interval)
	}
	row := Process{Identity: Identity{20, "root"}, Pane: 1, RSS: 1024 * 1024}
	roots := Roots{1: row.Identity}
	at := time.Unix(100, 0)
	var gradual Detector
	for i := range 5 {
		sample := row
		sample.RSS += int64(i) * cfg.SpikeKiB / 4
		m := gradual.Observe(at.Add(time.Duration(i)*cfg.Interval), cfg, roots, []Process{sample})[0]
		if m.Breach || m.Spike > cfg.SpikeKiB/2 {
			t.Fatalf("gradual growth at poll %d treated as a spike: %+v", i, m)
		}
	}
	var abrupt Detector
	abrupt.Observe(at, cfg, roots, []Process{row})
	// The poll starts at 15s, but collection finishes 14s later.
	row.RSS += cfg.SpikeKiB
	m := abrupt.Observe(at.Add(cfg.Interval+14*time.Second), cfg, roots, []Process{row})[0]
	if !m.Breach || m.Spike != cfg.SpikeKiB {
		t.Fatalf("abrupt growth with a delayed sample: %+v", m)
	}
}
