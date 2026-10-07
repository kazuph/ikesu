// Package platform collects the OS-specific bits gtx needs: foreground
// process of a PTY, process names, argv, cwd, and input flushing.
// Everything else in the codebase stays platform-neutral.
package platform

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
