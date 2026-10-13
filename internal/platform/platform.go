// Package platform collects the OS-specific bits gtx needs: foreground
// process of a PTY, process names, argv, cwd, and input flushing.
// Everything else in the codebase stays platform-neutral.
package platform

import (
	"github.com/kazuph/goterrace/internal/childenv"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ForegroundPID returns the process-group leader pid of the process in the
// foreground of the terminal behind fd (a PTY master), or 0 on failure.
func ForegroundPID(fd int) int { return foregroundPID(fd) }

// ProcName returns the short process name (comm) of pid.
func ProcName(pid int) string { return procName(pid) }

// ProcArgs returns the argv of pid, best effort.
func ProcArgs(pid int) []string { return procArgs(pid) }

// ProcCwd returns the current working directory of pid, best effort.
func ProcCwd(pid int) string { return procCwd(pid) }

// FlushInput discards pending terminal input on fd.
func FlushInput(fd int) { flushInput(fd) }

// ForegroundProcess describes one member of a foreground process group.
type ForegroundProcess struct {
	PID     int
	Name    string   // comm / short executable name
	Argv    []string // argv, best effort; Argv[0] is argv0
	Cmdline string   // argv joined with spaces, best effort
}

// ForegroundJob is the set of processes in the terminal's foreground
// process group.
type ForegroundJob struct {
	ProcessGroupID int
	Processes      []ForegroundProcess
}

// ForegroundJobOf returns the foreground process group of the terminal
// behind fd (a PTY master), or an empty job on failure.
func ForegroundJobOf(fd int) ForegroundJob { return foregroundJob(fd) }

// PPID returns the parent pid of pid, or 0 when unknown. Implemented via
// `ps` so it works on both darwin and linux without extra plumbing; this is
// called a handful of times per pane.current request, not in hot loops.
func PPID(pid int) int {
	out, err := childenv.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}

// ProcStart returns the start time of pid, best effort, via `ps etimes`.
// Used by the session-file observer to correlate a freshly created agent
// session file with the process that started it.
func ProcStart(pid int) (time.Time, bool) {
	if runtime.GOOS == "darwin" {
		cmd := childenv.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(pid))
		cmd.Env = append(childenv.Scrub(os.Environ()), "LC_ALL=C")
		out, err := cmd.Output()
		if err != nil {
			return time.Time{}, false
		}
		ts, err := time.ParseInLocation("Mon Jan _2 15:04:05 2006", strings.TrimSpace(string(out)), time.Local)
		return ts, err == nil
	}

	out, err := childenv.Command("ps", "-o", "etimes=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return time.Time{}, false
	}
	secs, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return time.Time{}, false
	}
	return time.Now().Add(-time.Duration(secs) * time.Second), true
}
