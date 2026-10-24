package server

import (
	"encoding/gob"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/kazuph/ikesu/internal/childenv"
	"github.com/kazuph/ikesu/internal/dispatch"
	"golang.org/x/sys/unix"
)

const jobSupervisorArg = "__job-supervisor"

type jobMonitorExit struct {
	JobID, Token          string
	MonitorPID            int
	MonitorIdentity       string
	StartedMs, ObservedMs int64
	ExitCode              *int
	Signal                *string
	Error                 string
}

// RunJobSupervisor owns Wait across server handoffs. It only relays the
// startup protocol and records evidence; it never decides job completion.
func RunJobSupervisor() (int, error) {
	f := os.NewFile(3, "supervisor-startup")
	up, err := jobControlConn(f)
	if err != nil {
		return 1, err
	}
	defer up.Close()
	up.SetDeadline(time.Now().Add(handoffWait))
	ue, ud := gob.NewEncoder(up), gob.NewDecoder(up)
	fail := func(err error) (int, error) { _ = ue.Encode(jobRunnerStarted{Error: err.Error()}); return 1, err }
	var req jobRunnerRequest
	if err = ud.Decode(&req); err != nil {
		return 1, err
	}
	req.SupervisorPID = os.Getpid()
	if req.SupervisorIdentity, err = jobProcessIdentity(req.SupervisorPID); err != nil {
		return fail(err)
	}
	if req.BootID, err = jobBootIdentity(); err != nil {
		return fail(err)
	}
	stderr, err := openJobLog(req.LogPath + ".monitor-stderr.log")
	if err != nil {
		return fail(err)
	}
	defer stderr.Close()
	// Open the parent before spawn, so late evidence can never create a retired log.
	parentLog, err := openJobLog(req.LogPath)
	if err != nil {
		return fail(err)
	}
	parentLog.Close()
	syscall.ForkLock.RLock()
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err == nil {
		unix.CloseOnExec(pair[0])
		unix.CloseOnExec(pair[1])
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		return fail(err)
	}
	a, b := os.NewFile(uintptr(pair[0]), "monitor-parent"), os.NewFile(uintptr(pair[1]), "monitor-child")
	down, err := jobControlConn(a)
	if err != nil {
		b.Close()
		return fail(err)
	}
	defer down.Close()
	down.SetDeadline(time.Now().Add(handoffWait))
	exe, err := os.Executable()
	if err != nil {
		b.Close()
		return fail(err)
	}
	cmd := childenv.Command(exe, jobRunnerArg)
	cmd.ExtraFiles = []*os.File{b}
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	err = cmd.Start()
	b.Close()
	if err != nil {
		return fail(err)
	}
	evidence := jobMonitorExit{JobID: req.JobID, Token: req.Token, MonitorPID: cmd.Process.Pid}
	evidence.MonitorIdentity, err = jobProcessIdentity(cmd.Process.Pid)
	de, dd := gob.NewEncoder(down), gob.NewDecoder(down)
	relay := func() error {
		if err != nil {
			return err
		}
		if e := de.Encode(req); e != nil {
			return e
		}
		var started jobRunnerStarted
		if e := dd.Decode(&started); e != nil {
			return e
		}
		if e := ue.Encode(started); e != nil {
			return e
		}
		if started.Error != "" {
			return fmt.Errorf("%s", started.Error)
		}
		var commit jobRunnerCommit
		if e := ud.Decode(&commit); e != nil {
			return e
		}
		evidence.StartedMs = commit.StartedMs
		if e := de.Encode(commit); e != nil {
			return e
		}
		if e := dd.Decode(&started); e != nil {
			return e
		}
		if e := ue.Encode(started); e != nil {
			return e
		}
		if started.Error != "" {
			return fmt.Errorf("%s", started.Error)
		}
		var release jobRunnerRelease
		if e := ud.Decode(&release); e != nil {
			return e
		}
		return de.Encode(release)
	}
	relayErr := relay()
	down.Close()
	up.Close()
	waitErr := cmd.Wait()
	evidence.ObservedMs = time.Now().UnixMilli()
	if waitErr != nil {
		evidence.Error = waitErr.Error()
	}
	if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		s := status.Signal().String()
		evidence.Signal = &s
	} else {
		v := cmd.ProcessState.ExitCode()
		evidence.ExitCode = &v
	}
	err = dispatch.WithJobLogLock(req.LogPath, func() error { return writeJobRecord(req.LogPath+".monitor-exit.json", evidence) })
	if err != nil && !os.IsNotExist(err) {
		return 1, err
	}
	if relayErr != nil {
		return 1, relayErr
	}
	return 0, nil
}
