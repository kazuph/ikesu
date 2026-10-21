//go:build darwin

package server

import (
	"fmt"
	"golang.org/x/sys/unix"
)

func jobProcessIdentity(pid int) (string, error) {
	proc, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%d", proc.Proc.P_starttime.Sec, proc.Proc.P_starttime.Usec), nil
}

// The existing Darwin kill(-pgid, 0) probe already decides group liveness.
func processGroupHasLiveMembers(int) bool { return true }
