//go:build perf && linux

package perf

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// /proc/<pid>/stat ABI field numbers, with the parenthesized comm counted
// as field 2. CPU fields are clock ticks, RSS is pages, not bytes or kB.
const (
	linuxStateField       = 3
	linuxParentField      = 4
	linuxUserTicksField   = 14
	linuxSystemTicksField = 15
	linuxRSSPagesField    = 24
)

var linuxClockTicks = sync.OnceValue(func() float64 {
	output, err := exec.Command("getconf", "CLK_TCK").Output()
	if err != nil {
		panic(fmt.Errorf("Linux perf clock frequency: %w", err))
	}
	ticks, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if err != nil || ticks <= 0 {
		panic(fmt.Errorf("Linux perf invalid CLK_TCK: %q", output))
	}
	return ticks
})

func psTable() map[int]procInfo {
	ticks := linuxClockTicks()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		panic(fmt.Errorf("Linux perf process table: %w", err))
	}
	table := map[int]procInfo{}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		directory := filepath.Join("/proc", entry.Name())
		data, err := os.ReadFile(filepath.Join(directory, "stat"))
		if err != nil {
			continue
		} // Processes may exit during the snapshot.
		end := strings.LastIndexByte(string(data), ')')
		if end < 0 {
			continue
		}
		fields := strings.Fields(string(data[end+1:]))
		if len(fields) <= linuxRSSPagesField-linuxStateField {
			continue
		}
		if fields[0] == "Z" || fields[0] == "X" {
			continue
		}
		parent, e1 := strconv.Atoi(fields[linuxParentField-linuxStateField])
		user, e2 := strconv.ParseUint(fields[linuxUserTicksField-linuxStateField], 10, 64)
		system, e3 := strconv.ParseUint(fields[linuxSystemTicksField-linuxStateField], 10, 64)
		pages, e4 := strconv.ParseInt(fields[linuxRSSPagesField-linuxStateField], 10, 64)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
			continue
		}
		// Linux comm is truncated and ps does not give the executable's full
		// path. The exe link preserves binary matching for client and server.
		executable, _ := os.Readlink(filepath.Join(directory, "exe"))
		table[pid] = procInfo{ppid: parent, rssKB: pages * int64(os.Getpagesize()) / 1024, cpuSec: float64(user+system) / ticks, comm: executable}
	}
	return table
}

func load1() float64 {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		panic(fmt.Errorf("Linux perf load average: %w", err))
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		panic("Linux perf empty /proc/loadavg")
	}
	load, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		panic(fmt.Errorf("Linux perf load average: %w", err))
	}
	return load
}
