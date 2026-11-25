package config

import (
	"testing"
	"time"
)

func TestRunawayDefaultsAndOptIn(t *testing.T) {
	t.Parallel()
	for _, data := range []string{"", "config_version=1", "config_version=2", DefaultExample} {
		cfg, err := Decode([]byte(data))
		if err != nil {
			t.Fatal(err)
		}
		want := RunawaySettings{Interval: 5 * time.Second, MemoryKiB: 8192 * 1024, SpikeKiB: 3072 * 1024, CPUPercent: 180, CPUDuration: 180 * time.Second}
		if got := cfg.RunawaySettings(); got != want {
			t.Fatalf("defaults = %+v, want %+v", got, want)
		}
	}
	cfg, err := Decode([]byte("config_version = 2\n[runaway]\nenabled=true\npoll_interval_seconds=1\nmemory_mb=16\nmemory_spike_mb=8\ncpu_percent=90\ncpu_duration_seconds=3\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := RunawaySettings{Enabled: true, Interval: 2 * time.Second, MemoryKiB: 16 * 1024, SpikeKiB: 8 * 1024, CPUPercent: 90, CPUDuration: 3 * time.Second}
	if got := cfg.RunawaySettings(); got != want {
		t.Fatalf("configured = %+v, want %+v", got, want)
	}
}
