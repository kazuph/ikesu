//go:build linux

package server

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func jobProcessIdentity(pid int) (string, error) {
	info, err := jobProcessState(pid)
	return info.identity, err
}

func jobProcessState(pid int) (jobProcessInfo, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return jobProcessInfo{}, err
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return jobProcessInfo{}, fmt.Errorf("invalid process stat")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) <= 19 {
		return jobProcessInfo{}, fmt.Errorf("missing process start time")
	}
	pgid, err := strconv.Atoi(fields[2])
	if err != nil {
		return jobProcessInfo{}, err
	}
	return jobProcessInfo{identity: fields[19], exited: fields[0] == "Z" || fields[0] == "X", pgid: pgid}, nil
}

// Linux keeps an unreaped zombie in its process group. It cannot run or
// receive a signal, so it must not keep a workspace dot or cancellation alive.
func processGroupHasLiveMembers(pgid int) bool {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return true
	}
	unknown := false
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		data, err := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if err != nil {
			if !os.IsNotExist(err) {
				unknown = true
			}
			continue
		}
		// comm is parenthesized and may itself contain spaces or parentheses.
		end := strings.LastIndexByte(string(data), ')')
		if end < 0 {
			continue
		}
		fields := strings.Fields(string(data[end+1:]))
		if len(fields) < 3 {
			continue
		}
		group, err := strconv.Atoi(fields[2])
		if err == nil && group == pgid && fields[0] != "Z" && fields[0] != "X" {
			return true
		}
	}
	return unknown
}

func jobBootIdentity() (string, error) {
	b, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
	return strings.TrimSpace(string(b)), e
}
