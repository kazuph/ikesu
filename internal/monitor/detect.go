package monitor

import (
	"time"

	"github.com/kazuph/ikesu/internal/config"
)

const spikeWindow = 30 * time.Second

const SnoozeDuration = 15 * time.Minute

type Measurement struct {
	Process
	Spike     int64
	TreeRSS   int64
	TreeSpike int64
	Breach    bool
	Percent   float64
	HighCPU   time.Duration
}

type reading struct {
	at  time.Time
	rss int64
}

type history struct {
	rss       []reading
	last      time.Time
	cpu       time.Duration
	highSince time.Time
}

type trackKey struct {
	pane int
	id   Identity
}

type Detector struct {
	processes map[trackKey]history
	trees     map[trackKey]history
}

// Observe returns current measurements and threshold breaches. All process history and
// rolling tree totals are scoped to the same process and pane identities.
func (d *Detector) Observe(now time.Time, cfg config.RunawaySettings, roots Roots, rows []Process) []Measurement {
	processes := make(map[trackKey]history, len(rows))
	trees := make(map[trackKey]history, len(roots))
	totals := make(map[int]int64, len(roots))
	largest := make(map[int]Process, len(roots))
	for _, row := range rows {
		totals[row.Pane] += row.RSS
		top := largest[row.Pane]
		if row.RSS > top.RSS || row.RSS == top.RSS && (top.PID == 0 || row.PID < top.PID) {
			largest[row.Pane] = row
		}
	}
	treeSpikes := make(map[int]int64, len(totals))
	for pane, rss := range totals {
		key := trackKey{pane, roots[pane]}
		h := d.trees[key]
		treeSpikes[pane] = h.memory(now, rss)
		trees[key] = h
	}
	measurements := make([]Measurement, 0, len(rows))
	for _, row := range rows {
		key := trackKey{row.Pane, row.Identity}
		h := d.processes[key]
		m := Measurement{Process: row, Spike: h.memory(now, row.RSS), TreeRSS: totals[row.Pane], TreeSpike: treeSpikes[row.Pane]}
		if interval := now.Sub(h.last); !h.last.IsZero() && interval > 0 && interval-cfg.Interval <= cfg.Interval && row.CPU >= h.cpu {
			m.Percent = 100 * (row.CPU - h.cpu).Seconds() / interval.Seconds()
			if m.Percent >= cfg.CPUPercent {
				if h.highSince.IsZero() {
					h.highSince = h.last
				}
				m.HighCPU = now.Sub(h.highSince)
			} else {
				h.highSince = time.Time{}
			}
		} else {
			h.highSince = time.Time{}
		}
		h.cpu, h.last = row.CPU, now
		processes[key] = h
		treeBreach := row.PID == largest[row.Pane].PID && (m.TreeRSS > cfg.MemoryKiB || treeSpikes[row.Pane] >= cfg.SpikeKiB)
		m.Breach = treeBreach || row.RSS > cfg.MemoryKiB || m.Spike >= cfg.SpikeKiB || m.HighCPU >= cfg.CPUDuration
		measurements = append(measurements, m)
	}
	d.processes, d.trees = processes, trees
	return measurements
}

func (h *history) memory(now time.Time, rss int64) int64 {
	first := 0
	for first < len(h.rss) && now.Sub(h.rss[first].at) > spikeWindow {
		first++
	}
	h.rss = h.rss[first:]
	// Keep a monotone queue of rolling minima. Higher, older readings
	// cannot become the minimum before this reading expires.
	base := rss
	if len(h.rss) > 0 {
		base = min(base, h.rss[0].rss)
	}
	for len(h.rss) > 0 && h.rss[len(h.rss)-1].rss >= rss {
		h.rss = h.rss[:len(h.rss)-1]
	}
	h.rss = append(h.rss, reading{now, rss})
	return rss - base
}
