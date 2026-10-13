//go:build linux

package server

import (
	"os"
	"strconv"
	"strings"
)

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
