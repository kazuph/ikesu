package server

import (
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/kazuph/ikesu/internal/dispatch"
)

func jobBootChanged(m jobRunnerRecord) (bool, error) {
	if m.BootID == "" {
		return false, nil
	}
	current, err := jobBootIdentity()
	if err != nil {
		return false, err
	}
	return current != m.BootID, nil
}

func managedJobGroupAlive(m jobRunnerRecord) (bool, error) {
	changed, err := jobBootChanged(m)
	if err != nil || changed {
		return false, err
	}
	return processGroupAlive(m.PID), nil
}

func monitorProcessGone(pid int, identity string) (bool, error) {
	if pid <= 0 || identity == "" {
		return false, fmt.Errorf("monitor identity unavailable")
	}
	if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
		return true, nil
	}
	current, err := jobProcessState(pid)
	if os.IsNotExist(err) || err == syscall.ESRCH {
		return true, nil
	}
	if err != nil {
		if syscall.Kill(pid, 0) == syscall.ESRCH {
			return true, nil
		}
		return false, fmt.Errorf("cannot establish runner monitor identity: %w", err)
	}
	return current.identity != identity || current.exited, nil
}

// Recovery never signals a process. Evidence publication is deliberately not a
// prerequisite: a stopped reaper or an unwritable log must not strand a job.
func (s *Server) recoverLostJob(store *dispatch.Store, job dispatch.Job, m jobRunnerRecord) error {
	s.jobProcs.cancelMu.Lock()
	shouldNudge := false
	defer func() {
		// run.cancel takes cancelMu on the app loop. Never wait for that
		// loop while still owning the mutex it needs.
		s.jobProcs.cancelMu.Unlock()
		if shouldNudge {
			s.app.Do(func() { s.flushJobNudgeFor(dispatch.JobsRoom, job.CallerPane) })
		}
	}()
	current, err := store.CommandRow(job.ID)
	if err != nil {
		return err
	}
	if current == nil || current.Status != "running" {
		return nil
	}
	gone, err := jobMonitorGone(m)
	if err != nil || !gone {
		return err
	}
	alive, err := managedJobGroupAlive(m)
	if err != nil || alive {
		return err
	}
	if _, err = completedJobRecord(job, m); err == nil {
		s.invalidateJobCache()
		return nil
	}
	reason := "completion unavailable: runner monitor disappeared"
	if !os.IsNotExist(err) {
		reason += "; " + err.Error()
	}
	finished := time.Now().UnixMilli()
	diagnostic := struct {
		JobID, Token, Reason string
		MonitorPID           int
		MonitorIdentity      string
		ObservedMs           int64
	}{job.ID, m.Token, reason, m.MonitorPID, m.MonitorIdentity, finished}
	if e := dispatch.TryWithJobLogLock(job.LogPath, func() error { return writeJobRecord(job.LogPath+".completion-unavailable.json", diagnostic) }); e != nil {
		reason += "; diagnostic save failed: " + e.Error()
		fmt.Fprintf(os.Stderr, "ikesu job %s: %s\n", job.ID, reason)
	}
	body := ""
	if job.Completion != "none" {
		body = fmt.Sprintf("[ikesu run] exit=unknown label=%s job=%s details: ikesu log %s reason: %s", labelOrDash(job.Label), job.ID, job.ID, reason)
		if job.Completion == "full" {
			if tail := readLogTail(job.LogPath, 40); tail != "" {
				body += "\n" + tail
			}
		}
	}
	changed, err := store.CompleteJob(job.ID, "running", "exited", nil, finished, job.CallerPane, body)
	shouldNudge = err == nil && changed && body != ""
	return err
}
