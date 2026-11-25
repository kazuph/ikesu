package monitor

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

func executablePath(pid int) (string, error) {
	// XNU proc_info uses these arguments for libproc's proc_pidpath.
	const procInfoCallPIDInfo, procPIDPathInfo = 2, 11
	var path [4096]byte // PROC_PIDPATHINFO_MAXSIZE
	_, _, err := unix.Syscall6(unix.SYS_PROC_INFO, procInfoCallPIDInfo, uintptr(pid), procPIDPathInfo,
		0, uintptr(unsafe.Pointer(&path[0])), uintptr(len(path)))
	if err != 0 {
		return "", err
	}
	return unix.ByteSliceToString(path[:]), nil
}

func darwinProcess(p unix.KinfoProc) Process {
	start := p.Proc.P_starttime
	return Process{
		Identity: Identity{int(p.Proc.P_pid), fmt.Sprintf("%d:%d", start.Sec, start.Usec)},
		Parent:   int(p.Eproc.Ppid), Group: int(p.Eproc.Pgid),
	}
}

func processInfo(pid int) (Process, error) {
	p, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return Process{}, err
	}
	if p.Proc.P_pid != int32(pid) || p.Proc.P_stat == 5 { // SZOMB
		return Process{}, unix.ESRCH
	}
	return darwinProcess(*p), nil
}

func sampleReader() (readSample, error) {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, err
	}
	// Bracket the ps counters with exact kernel birth times. A PID that
	// changes or first appears during ps cannot inherit its measurements.
	before := make(map[int]Process, len(procs))
	for _, p := range procs {
		if p.Proc.P_pid > 1 && p.Proc.P_stat != 5 {
			before[int(p.Proc.P_pid)] = darwinProcess(p)
		}
	}
	return bracketSample(before, processInfo), nil
}

func signalProcess(target Target, root Identity, roots Roots, sig unix.Signal) error {
	return signalBound(target, root, roots, sig, processInfo, Executable, unix.Getsid, unix.Kill)
}
