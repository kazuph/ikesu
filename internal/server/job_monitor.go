package server

import (
	"encoding/json"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/dispatch"
)

type jobProcessInfo struct {
	identity string
	exited   bool
	pgid     int
}

// Admission and shutdown share the mutex: Add cannot race a shutdown Wait.
func (s *Server) startJobMonitor(id string, work func(<-chan struct{}) error) {
	j := &s.jobProcs
	j.mu.Lock()
	if j.stopping || s.closing.Load() != 0 || j.cancelling[id] {
		j.mu.Unlock()
		return
	}
	if j.cancelling == nil {
		j.cancelling = map[string]bool{}
	}
	if j.stop == nil {
		j.stop = make(chan struct{})
	}
	j.cancelling[id] = true
	stop := j.stop
	j.wg.Add(1)
	j.mu.Unlock()
	go func() {
		defer j.wg.Done()
		defer func() { j.mu.Lock(); delete(j.cancelling, id); j.mu.Unlock() }()
		if jobMonitorStopped(stop) {
			return
		}
		if err := work(stop); err != nil {
			fmt.Fprintf(os.Stderr, "ikesu job cancellation %s: %v\n", id, err)
		}
	}()
}

func (s *Server) stopJobMonitors() {
	j := &s.jobProcs
	j.mu.Lock()
	if !j.stopping {
		j.stopping = true
		if j.stop != nil {
			close(j.stop)
		}
	}
	j.mu.Unlock()
	j.wg.Wait()
}

func (s *Server) restartJobMonitors() {
	j := &s.jobProcs
	j.mu.Lock()
	defer j.mu.Unlock()
	if s.closing.Load() == 0 {
		j.stopping = false
		j.stop = make(chan struct{})
	}
}

func jobMonitorStopped(stop <-chan struct{}) bool {
	select {
	case <-stop:
		return true
	default:
		return false
	}
}

func waitJobGroupGone(pid int, timeout time.Duration, stop <-chan struct{}) bool {
	if jobMonitorStopped(stop) {
		return false
	}
	if !processGroupAlive(pid) {
		return true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return false
		case <-timer.C:
			return !processGroupAlive(pid)
		case <-tick.C:
			if !processGroupAlive(pid) {
				return true
			}
		}
	}
}

func verifyJobGroup(manifest jobRunnerRecord) error {
	if changed, err := jobBootChanged(manifest); err != nil {
		return err
	} else if changed {
		return fmt.Errorf("job belongs to another OS boot; refusing to signal")
	}
	if manifest.PID <= 0 {
		return fmt.Errorf("invalid runner PID; refusing to signal")
	}
	info, err := jobProcessState(manifest.PID)
	if err != nil {
		return fmt.Errorf("process start identity unavailable; refusing to signal: %w", err)
	}
	if info.identity != manifest.Identity {
		return fmt.Errorf("runner process identity changed; refusing to signal")
	}
	if info.pgid != manifest.PID {
		return fmt.Errorf("runner no longer leads its process group; refusing to signal")
	}
	return nil
}

func signalManagedJob(manifest jobRunnerRecord, signal syscall.Signal) error {
	if err := verifyJobGroup(manifest); err != nil {
		return err
	}
	return syscall.Kill(-manifest.PID, signal)
}

// A reaped leader does not revoke ownership of its surviving process group.
// A live leader still has to match, even when cancellation was verified earlier.
func signalCancellationGroup(manifest jobRunnerRecord, state jobCancelState, signal syscall.Signal) error {
	if changed, err := jobBootChanged(manifest); err != nil {
		return err
	} else if changed {
		return fmt.Errorf("job belongs to another OS boot; refusing to signal")
	}
	if manifest.PID > 0 && state.VerifiedPGID == manifest.PID && state.VerifiedIdentity != "" && state.VerifiedIdentity == manifest.Identity && syscall.Kill(manifest.PID, 0) == syscall.ESRCH {
		return syscall.Kill(-manifest.PID, signal)
	}
	return signalManagedJob(manifest, signal)
}

func jobMonitorGone(manifest jobRunnerRecord) (bool, error) {
	if changed, err := jobBootChanged(manifest); err != nil || changed {
		return changed, err
	}
	return monitorProcessGone(manifest.MonitorPID, manifest.MonitorIdentity)
}

type jobCancelState struct {
	TermSentMs       int64
	VerifiedPGID     int
	VerifiedIdentity string
}

// cancelMu serializes preparation, persistence and signalling with recovery.
func (s *Server) runManagedCancel(id string, store *dispatch.Store, job dispatch.Job, manifest jobRunnerRecord) api.Response {
	s.jobProcs.cancelMu.Lock()
	defer s.jobProcs.cancelMu.Unlock()
	alive, probeErr := managedJobGroupAlive(manifest)
	if probeErr != nil {
		return api.Failure(id, api.ErrJobStore, probeErr.Error())
	}
	var state jobCancelState
	if alive {
		if err := verifyJobGroup(manifest); err != nil {
			return api.Failure(id, api.ErrJobStore, err.Error())
		}
		state.VerifiedPGID, state.VerifiedIdentity = manifest.PID, manifest.Identity
	}
	// A preparation failure must leave the public running state untouched.
	if err := writeJobRecord(job.LogPath+".cancel", state); err != nil {
		return api.Failure(id, api.ErrJobStore, err.Error())
	}
	ok, err := store.MarkCommandCancelling(job.ID)
	if err != nil || !ok {
		_ = os.Remove(job.LogPath + ".cancel")
		if err != nil {
			return api.Failure(id, api.ErrJobStore, err.Error())
		}
		return api.Failure(id, api.ErrJobNotFound, "job is no longer running")
	}
	signal := "none"
	if alive {
		if err := signalCancellationGroup(manifest, state, syscall.SIGTERM); err != nil {
			s.resumeJobCancel(job.ID, manifest)
			return api.Failure(id, api.ErrJobStore, err.Error())
		}
		signal = "TERM"
		state.TermSentMs = time.Now().UnixMilli()
		if err := writeJobRecord(job.LogPath+".cancel", state); err != nil {
			s.resumeJobCancel(job.ID, manifest)
			return api.Failure(id, api.ErrJobStore, err.Error())
		}
	}
	s.invalidateJobCache()
	s.resumeJobCancel(job.ID, manifest)
	return api.Success(id, api.RunCancelledResult{Type: "run_cancelled", Job: job.ID, Status: "cancelling", Signal: signal})
}

func (s *Server) resumeJobCancel(id string, manifest jobRunnerRecord) {
	s.startJobMonitor(id, func(stop <-chan struct{}) error {
		store, err := s.db()
		if err != nil {
			return err
		}
		job, err := store.CommandRow(id)
		if err != nil {
			return err
		}
		if job == nil || job.Status != "cancelling" {
			return nil
		}
		if changed, err := jobBootChanged(manifest); err != nil {
			return err
		} else if changed {
			return s.recordCancellation(store, *job, false)
		}
		s.jobProcs.cancelMu.Lock()
		if jobMonitorStopped(stop) {
			s.jobProcs.cancelMu.Unlock()
			return nil
		}
		var state jobCancelState
		data, err := os.ReadFile(job.LogPath + ".cancel")
		// Empty is the existing cancellation intent; it has no durable TERM acknowledgement.
		if err == nil && len(data) > 0 {
			err = json.Unmarshal(data, &state)
		}
		if os.IsNotExist(err) {
			err = nil
		}
		if err == nil && state.TermSentMs == 0 && processGroupAlive(manifest.PID) {
			if state.VerifiedPGID != manifest.PID || state.VerifiedIdentity != manifest.Identity {
				err = verifyJobGroup(manifest)
				if err == nil {
					state.VerifiedPGID, state.VerifiedIdentity = manifest.PID, manifest.Identity
					err = writeJobRecord(job.LogPath+".cancel", state)
				}
			}
			if err == nil {
				err = signalCancellationGroup(manifest, state, syscall.SIGTERM)
			}
			if err == nil {
				state.TermSentMs = time.Now().UnixMilli()
				err = writeJobRecord(job.LogPath+".cancel", state)
			}
		}
		s.jobProcs.cancelMu.Unlock()
		if err != nil {
			return err
		}
		remaining := time.Until(time.UnixMilli(state.TermSentMs).Add(2 * time.Second))
		if !waitJobGroupGone(manifest.PID, max(0, remaining), stop) {
			if jobMonitorStopped(stop) {
				return nil
			}
			if err := signalCancellationGroup(manifest, state, syscall.SIGKILL); err != nil {
				return err
			}
			if !waitJobGroupGone(manifest.PID, 30*time.Second, stop) {
				return nil
			}
		}
		if jobMonitorStopped(stop) {
			return nil
		}
		// Owned until this callback returns; cleanup cannot close store here.
		current, err := store.CommandRow(id)
		if err != nil {
			return err
		}
		if current == nil || current.Status != "cancelling" {
			return nil
		}
		return s.recordCancellation(store, *current, false)
	})
}
