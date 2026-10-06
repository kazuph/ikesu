//go:build darwin

package platform

import (
	"bytes"
	"encoding/binary"
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
// (padded to int alignment), then argv[0..argc-1] as NUL-terminated strings.
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
	// Align to int boundary.
	pos = (pos + 3) &^ 3
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

func flushInput(fd int) {
	_ = unix.IoctlSetPointerInt(fd, unix.TIOCFLUSH, unix.TCIFLUSH)
}
