//go:build linux

package platform

import (
	"os"
	"strings"

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
	b, err := os.ReadFile("/proc/" + itoa(pid) + "/comm")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func procArgs(pid int) []string {
	b, err := os.ReadFile("/proc/" + itoa(pid) + "/cmdline")
	if err != nil {
		return nil
	}
	parts := strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func procCwd(pid int) string {
	p, err := os.Readlink("/proc/" + itoa(pid) + "/cwd")
	if err != nil {
		return ""
	}
	return p
}

func flushInput(fd int) {
	_ = unix.IoctlSetPointerInt(fd, unix.TCFLSH, unix.TCIFLUSH)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [16]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}
