//go:build darwin

package guard

import (
	"fmt"
	"golang.org/x/sys/unix"
	"syscall"
)

func approvalProcess(pid int) (int, string, error) {
	process, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return 0, "", err
	}
	if process.Proc.P_pid == 0 {
		return 0, "", syscall.ESRCH
	}
	return int(process.Eproc.Ppid), fmt.Sprintf("%d:%d", process.Proc.P_starttime.Sec, process.Proc.P_starttime.Usec), nil
}

func approvalExecutable(pid int) (string, error) {
	data, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return "", err
	}
	if len(data) < 5 {
		return "", syscall.ESRCH
	}
	for end := 4; end < len(data); end++ {
		if data[end] == 0 {
			return string(data[4:end]), nil
		}
	}
	return "", syscall.ESRCH
}
