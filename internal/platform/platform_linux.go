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

// foregroundJob enumerates every process in the terminal's foreground
// process group by scanning /proc for entries whose pgrp matches.
func foregroundJob(fd int) ForegroundJob {
	pgid := foregroundPID(fd)
	if pgid <= 0 {
		return ForegroundJob{}
	}
	job := ForegroundJob{ProcessGroupID: pgid}
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return job
	}
	for _, e := range ents {
		pid := atoi(e.Name())
		if pid <= 0 {
			continue
		}
		if procPgrp(pid) != pgid {
			continue
		}
		name := procName(pid)
		if name == "" {
			continue
		}
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

// procPgrp parses the pgrp field (field 5) of /proc/<pid>/stat; comm may
// contain spaces or parens, so parsing starts after the last ')'.
func procPgrp(pid int) int {
	b, err := os.ReadFile("/proc/" + itoa(pid) + "/stat")
	if err != nil {
		return -1
	}
	s := string(b)
	i := strings.LastIndex(s, ")")
	if i < 0 {
		return -1
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 3 {
		return -1
	}
	return atoi(f[2])
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
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
