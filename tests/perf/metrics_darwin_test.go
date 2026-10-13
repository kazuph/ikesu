//go:build perf && darwin

package perf

import (
	"os/exec"
	"strconv"
	"strings"
)

func psTable() map[int]procInfo {
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,rss=,time=,comm=").Output()
	if err != nil {
		return nil
	}
	tbl := map[int]procInfo{}
	for _, ln := range strings.Split(string(out), "\n") {
		f := strings.Fields(ln)
		if len(f) < 5 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		rss, err3 := strconv.ParseInt(f[2], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		tbl[pid] = procInfo{ppid: ppid, rssKB: rss, cpuSec: parseCPUTime(f[3]), comm: strings.Join(f[4:], " ")}
	}
	return tbl
}

func parseCPUTime(s string) float64 {
	parts := strings.Split(s, ":")
	sec, mult := 0.0, 1.0
	for i := len(parts) - 1; i >= 0; i-- {
		v, _ := strconv.ParseFloat(parts[i], 64)
		sec += v * mult
		mult *= 60
	}
	return sec
}

func load1() float64 {
	out, err := exec.Command("sysctl", "-n", "vm.loadavg").Output()
	if err != nil {
		return -1
	}
	f := strings.Fields(strings.Trim(string(out), "{} \t\n"))
	if len(f) == 0 {
		return -1
	}
	v, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return -1
	}
	return v
}
