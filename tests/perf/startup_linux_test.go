//go:build perf && linux

package perf

import (
	"fmt"
	"strings"
	"testing"
)

// This checks only launch and Linux metric readers. It never runs tp.py,
// measures throughput/Ctrl-C, or evaluates absolute/relative limits.
func TestLinuxPerfStartupOnly(t *testing.T) {
	bin := perfBin(t)
	if load1() < 0 {
		t.Fatal("Linux load average unavailable")
	}
	h := newHarness(t)
	bins := binPaths(bin)
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			session := fmt.Sprintf("smoke-%s", mode.name)
			process := h.start(bin, mode.args(session))
			defer h.stop(process, session, mode.name == "server", bins)
			if _, ok := process.waitText(bootTimeout, "❯"); !ok {
				t.Fatal("startup prompt unavailable", process.screen())
			}
			table := psTable()
			pids := gtxPids(table, process.cmd.Process.Pid, bins)
			want := 1
			if mode.name == "server" {
				want = 2
			}
			if len(pids) != want {
				t.Fatal("client/server executable identification failed", pids)
			}
			for pid := range pids {
				info, ok := table[pid]
				if !ok || info.rssKB <= 0 || info.cpuSec < 0 || !bins[info.comm] {
					t.Fatal("Linux process metrics unavailable", pid, info)
				}
			}
			sampler := startSampler(process.cmd.Process.Pid, bins)
			rss, cpu, seen := sampler.finish()
			if rss <= 0 || cpu < 0 || len(strings.Split(seen, "+")) != want {
				t.Fatal("Linux sampler did not retain the started processes")
			}
			t.Log("startup and Linux metric readers validated; no workload or performance judgment executed")
		})
	}
}
