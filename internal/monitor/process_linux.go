package monitor

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func executablePath(pid int) (string, error) {
	path, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
	if err == nil && strings.HasSuffix(path, " (deleted)") {
		return "", errExecutable
	}
	return path, err
}

func processInfo(pid int) (Process, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return Process{}, err
	}
	text := string(data)
	end := strings.LastIndexByte(text, ')')
	if end < 0 {
		return Process{}, errors.New("invalid process stat")
	}
	fields := strings.Fields(text[end+1:])
	if len(fields) <= 21 || fields[0] == "Z" || fields[0] == "X" {
		return Process{}, unix.ESRCH
	}
	parent, err := strconv.Atoi(fields[1])
	user, userErr := strconv.ParseUint(fields[11], 10, 64)
	system, systemErr := strconv.ParseUint(fields[12], 10, 64)
	pages, rssErr := strconv.ParseInt(fields[21], 10, 64)
	if err != nil || userErr != nil || systemErr != nil || rssErr != nil || pages < 0 {
		return Process{}, errors.New("invalid process stat counters")
	}
	// procfs exports CPU time in USER_HZ (100 on the supported architectures).
	return Process{
		Identity: Identity{pid, fields[19]}, Parent: parent,
		RSS: pages * int64(os.Getpagesize()/1024), CPU: time.Duration(user+system) * (time.Second / 100),
	}, nil
}

func sampleReader() (readSample, error) {
	// ps discovers the tree; RSS, CPU, and start all come from one stat
	// record for the managed processes. No ps metric is attached to a PID.
	return func(row Process) (Process, error) { return processInfo(row.PID) }, nil
}

func signalProcess(target Target, root Identity, roots Roots, sig unix.Signal) error {
	fd, err := unix.PidfdOpen(target.PID, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err := validateOwner(target.Identity, root, roots, processInfo); err != nil {
		return err
	}
	if err := CheckTarget(target); err != nil {
		return err
	}
	return unix.PidfdSendSignal(fd, sig, nil, 0)
}
