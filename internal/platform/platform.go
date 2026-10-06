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
