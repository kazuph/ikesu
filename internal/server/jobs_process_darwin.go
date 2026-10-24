//go:build darwin

package server

import (
	"fmt"
	"golang.org/x/sys/unix"
	"syscall"
)

func jobProcessIdentity(pid int) (string, error) {
	proc, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return "", err
	}
	if proc.Proc.P_pid == 0 || proc.Proc.P_stat == darwinZombieState {
		return "", syscall.ESRCH
	}
	return fmt.Sprintf("%d:%d", proc.Proc.P_starttime.Sec, proc.Proc.P_starttime.Usec), nil
}

// SZOMB in the Darwin SDK sys/proc.h: an exited process awaiting its parent.
const darwinZombieState = 5

func processGroupHasLiveMembers(pgid int) bool {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return true
	}
	for _, p := range procs {
		if int(p.Eproc.Pgid) == pgid && p.Proc.P_stat != darwinZombieState {
			return true
		}
	}
	return false
}
func jobBootIdentity() (string, error) { return unix.Sysctl("kern.bootsessionuuid") }
