//go:build darwin

package platform

import (
	"bytes"
	"encoding/binary"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

func foregroundPID(fd int) int {
	pgid, err := unix.IoctlGetInt(fd, unix.TIOCGPGRP)
	if err != nil || pgid <= 0 {
		return 0
	}
	return pgid
}

func procName(pid int) string {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return ""
	}
	name := kp.Proc.P_comm[:]
	if i := bytes.IndexByte(name, 0); i >= 0 {
		name = name[:i]
	}
	return string(name)
}

// procArgs parses KERN_PROCARGS2: int32 argc, NUL-terminated exec path
// (followed by NUL padding), then argv[0..argc-1] as NUL-terminated strings.
func procArgs(pid int) []string {
	buf, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil || len(buf) < 8 {
		return nil
	}
	argc := int(binary.LittleEndian.Uint32(buf))
	if argc <= 0 || argc > 4096 {
		return nil
	}
	pos := 4
	// Skip the executable path.
	for pos < len(buf) && buf[pos] != 0 {
		pos++
	}
	pos++
	// The executable path is followed by NUL padding, not a fixed alignment.
	for pos < len(buf) && buf[pos] == 0 {
		pos++
	}
	args := make([]string, 0, argc)
	for len(args) < argc && pos < len(buf) {
		start := pos
		for pos < len(buf) && buf[pos] != 0 {
			pos++
		}
		args = append(args, string(buf[start:pos]))
		pos++
	}
	return args
}

// procCwd uses proc_info(PROC_PIDVNODEPATHINFO): the returned
// proc_vnodepathinfo is 2352 bytes and the cdir path sits at offset 152
// (verified empirically on macOS/arm64).
func procCwd(pid int) string {
	const (
		callPIDInfo         = 2
		flavorVnodePathInfo = 9
		vnodePathInfoSize   = 2352
		cdirPathOffset      = 152
		maxPath             = 1024
	)
	buf := make([]byte, vnodePathInfoSize+16)
	n, _, errno := unix.Syscall6(unix.SYS_PROC_INFO, callPIDInfo, uintptr(pid),
		flavorVnodePathInfo, 0,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if errno != 0 || n <= 0 {
		return ""
	}
	path := buf[cdirPathOffset : cdirPathOffset+maxPath]
	if i := bytes.IndexByte(path, 0); i >= 0 {
		path = path[:i]
	}
	if len(path) == 0 || path[0] != '/' {
		return ""
	}
	return string(path)
}

// foregroundJob enumerates every process in the terminal's foreground
// process group via proc_info(PROC_LISTPIDS, PROC_PGRP_ONLY).
func foregroundJob(fd int) ForegroundJob {
	pgid := foregroundPID(fd)
	if pgid <= 0 {
		return ForegroundJob{}
	}
	job := ForegroundJob{ProcessGroupID: pgid, Sudo: foregroundSudo(pgid)}
	for _, pid := range groupPids(pgid) {
		// Re-validate membership; the pid may have been reused or moved
		// between the listing and this check.
		if g, err := unix.Getpgid(pid); err != nil || g != pgid {
			continue
		}
		name := procName(pid)
		if name == "" {
			continue
		}
		job.Sudo = job.Sudo || name == "sudo"
		argv := procArgs(pid)
		job.Processes = append(job.Processes, ForegroundProcess{
			PID:     pid,
			Name:    name,
			Argv:    argv,
			Cmdline: strings.Join(argv, " "),
		})
	}
	return job
}

// groupPids lists the pids in process group pgid using the proc_info
// syscall directly (libproc's proc_listpids is a thin wrapper over
// proc_info(PROC_LISTPIDS), so no cgo is required).
func groupPids(pgid int) []int {
	const (
		callListPids = 1 // PROC_LISTPIDS
		typePgrpOnly = 2 // PROC_PGRP_ONLY
	)
	for capacity := 16; capacity <= 1<<16; capacity *= 4 {
		buf := make([]int32, capacity)
		n, _, errno := unix.Syscall6(unix.SYS_PROC_INFO, callListPids,
			typePgrpOnly, uintptr(pgid), 0,
			uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*4))
		if errno != 0 || n <= 0 {
			return nil
		}
		if int(n) < capacity {
			out := make([]int, n)
			for i := range out {
				out[i] = int(buf[i])
			}
			return out
		}
	}
	return nil
}

func flushInput(fd int) {
	_ = unix.IoctlSetPointerInt(fd, unix.TIOCFLUSH, unix.TCIFLUSH)
}

// A sudo monitor can remain outside the child's foreground group. Follow
// ancestors only across this terminal, including sudo at a PTY boundary.
func foregroundSudo(pid int) bool {
	seen := map[int]bool{}
	var tty int32
	for pid > 1 && !seen[pid] {
		seen[pid] = true
		kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
		if err != nil {
			return false
		}
		if bytes.Equal(bytes.TrimRight(kp.Proc.P_comm[:], "\x00"), []byte("sudo")) {
			return true
		}
		if len(seen) == 1 {
			tty = kp.Eproc.Tdev
		} else if kp.Eproc.Tdev != tty {
			return false
		}
		pid = int(kp.Eproc.Ppid)
	}
	return false
}
