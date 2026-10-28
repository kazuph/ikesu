//go:build linux

package guard

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func approvalProcess(pid int) (int, string, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, "", err
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return 0, "", fmt.Errorf("invalid process stat")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) <= 19 {
		return 0, "", fmt.Errorf("missing process identity")
	}
	parent, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, "", err
	}
	return parent, fields[19], nil
}

func approvalExecutable(pid int) (string, error) {
	return os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
}
