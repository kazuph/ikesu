package config

import "time"

type RunawayConfig struct {
	Enabled            *bool `toml:"enabled"`
	PollSeconds        int   `toml:"poll_interval_seconds"`
	MemoryMB           int   `toml:"memory_mb"`
	MemorySpikeMB      int   `toml:"memory_spike_mb"`
	CPUPercent         int   `toml:"cpu_percent"`
	CPUDurationSeconds int   `toml:"cpu_duration_seconds"`
}

type RunawaySettings struct {
	Enabled     bool
	Interval    time.Duration
	MemoryKiB   int64
	SpikeKiB    int64
	CPUPercent  float64
	CPUDuration time.Duration
}

func (c Config) RunawaySettings() RunawaySettings {
	r := c.Runaway
	positive := func(n, fallback int) int64 {
		if n <= 0 {
			return int64(fallback)
		}
		return int64(n)
	}
	duration := func(seconds int64) time.Duration {
		return time.Duration(min(seconds, int64((1<<63-1)/time.Second))) * time.Second
	}
	kib := func(mb int64) int64 { return min(mb, (1<<63-1)/1024) * 1024 }
	return RunawaySettings{
		Enabled:     r.Enabled != nil && *r.Enabled,
		Interval:    duration(min(15, max(2, positive(r.PollSeconds, 5)))),
		MemoryKiB:   kib(positive(r.MemoryMB, 8192)),
		SpikeKiB:    kib(positive(r.MemorySpikeMB, 3072)),
		CPUPercent:  float64(positive(r.CPUPercent, 180)),
		CPUDuration: duration(positive(r.CPUDurationSeconds, 180)),
	}
}
