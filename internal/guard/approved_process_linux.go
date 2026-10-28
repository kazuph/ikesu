//go:build linux

package guard

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
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

func executableFileIdentity(info os.FileInfo) executableIdentity {
	st := info.Sys().(*syscall.Stat_t)
	return executableIdentity{uint64(st.Dev), uint64(st.Ino), info.Size(), st.Mtim.Sec, st.Mtim.Nsec, st.Ctim.Sec, st.Ctim.Nsec}
}
