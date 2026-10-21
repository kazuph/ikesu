package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/dispatch"
)

// Pane-less background jobs (gtx run). A job is a command dispatch row in
// the SQLite store plus a detached child process whose output streams to a
// log file under StateDir()/job-logs. Completion writes one nudge into the
// caller's jobs-room mailbox; delivery follows the regular mailbox
// pipeline.

// jobProcs keeps server-local numbering and cancellation-monitor ownership.
// The runner process and its durable records survive this server.
type jobProcsT struct {
	mu         sync.Mutex
	seq        int // next external job number, seeded from the store
	cancelling map[string]bool
	stopping   bool
	stop       chan struct{}
	wg         sync.WaitGroup
	cancelMu   sync.Mutex
}

func (j *jobProcsT) nextID() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.seq++
	return fmt.Sprintf("j%04d", j.seq)
}

func (s *Server) seedJobSeq() {
	store, err := s.db()
	if err != nil {
		return
	}
	max, err := store.MaxJobSeq()
	if err == nil {
		s.jobProcs.mu.Lock()
		s.jobProcs.seq = int(max)
		s.jobProcs.mu.Unlock()
	}
}

func (s *Server) jobLogPath(jobID, label string) string {
	return dispatch.JobLogPath(jobID, label, s.session)
}

// runStart handles run.start: insert the row, spawn the detached process,
// and hand execution and logging to a detached runner.
func (s *Server) runStart(id string, p api.RunStartParams) api.Response {
	if len(p.Argv) == 0 || strings.TrimSpace(p.Argv[0]) == "" {
		return api.Failure(id, api.ErrInvalidRequest, "argv must not be empty")
	}
	completion := p.Completion
	if completion == "" {
		completion = "summary"
	}
	if completion != "summary" && completion != "full" && completion != "none" {
		return api.Failure(id, api.ErrInvalidRequest,
			`completion must be "summary", "full", or "none"`)
	}
	label := p.Label
	// Notifications require an agent mailbox; silent jobs only need a live pane.
	callerAgent := ""
	callerPane := ""
	if p.CallerPane == "" {
		return api.Failure(id, api.ErrInvalidRequest, "caller pane is required; specify --caller <pane>")
	}
	if p.CallerPane != "" {
		t, err := s.resolveTarget(p.CallerPane, completion != "none")
		if err != nil {
			return api.Failure(id, err.Code, err.Message)
		}
		callerPane = fmtPaneID(t.pid)
		callerAgent, _ = s.mailboxName(t.pid)
		if completion != "none" && (!s.isAgentPane(t.pid) || callerAgent == "") {
			return api.Failure(id, api.ErrAgentNotFound, "caller pane has no agent mailbox")
		}
	}
	cwd := s.cwd
	if p.Cwd != "" {
		cwd = p.Cwd
	}
	store, err := s.db()
	if err != nil {
		return api.Failure(id, api.ErrJobStore, err.Error())
	}
	cmdText := strings.Join(p.Argv, " ")
	jobID := s.jobProcs.nextID()
	logPath := s.jobLogPath(jobID, label)
	if err := store.InsertCommand(jobID, label, cmdText, cwd,
		callerPane, callerAgent, completion, logPath); err != nil {
		return api.Failure(id, api.ErrJobStore, err.Error())
	}

	if err := s.startJobRunner(store, jobID, logPath, cwd, p.Argv); err != nil {
		s.finishJob(store, jobID, nil)
		return api.Failure(id, api.ErrJobStore, err.Error())
	}
	s.invalidateJobCache()

	return api.Success(id, api.RunStartedResult{
		Type: "run_started", Job: jobID, Label: label, Mode: "background",
	})
}

// finishJob records the terminal state for a job that never ran; the
// row is still 'queued', so a plain finish is a no-op and it ends as
// 'cancelled'.
func (s *Server) finishJob(store *dispatch.Store, jobID string, exitCode *int64) {
	defer s.invalidateJobCache()
	if ok, _ := store.MarkCommandFinished(jobID, exitCode); !ok {
		_, _ = store.MarkCommandCancelled(jobID)
	}
}

func labelOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// runCancel handles run.cancel, following SPEC G9: the runner pid must be
// the leader of a process group of the same number; only then is the row
// moved to 'cancelling' and SIGTERM sent to the group. The response is
// 'cancelling' — finishCancel escalates to SIGKILL and only marks the job
// 'cancelled' once the whole group is gone.
func (s *Server) runCancel(id string, p api.RunCancelParams) api.Response {
	jobID := strings.TrimSpace(p.JobID)
	if jobID == "" {
		return api.Failure(id, api.ErrJobNotFound, "job_id must not be empty")
	}
	store, err := s.db()
	if err != nil {
		return api.Failure(id, api.ErrJobStore, err.Error())
	}
	job, err := store.CommandRow(jobID)
	if err != nil || job == nil {
		return api.Failure(id, api.ErrJobNotFound,
			fmt.Sprintf("job not found: %s", jobID))
	}
	if job.Status != "running" {
		return api.Failure(id, api.ErrJobNotFound,
			fmt.Sprintf("job %s is not running (status=%s)", job.ID, job.Status))
	}
	// The persisted PID retains the real command identity across handoffs.
	pgid := 0
	if job.RunnerPID != nil {
		pgid = int(*job.RunnerPID)
	}
	if pgid <= 0 {
		// No runner pid recorded — nothing left to signal.
		s.recordCancellation(store, *job, true)
		return api.Success(id, api.RunCancelledResult{
			Type: "run_cancelled", Job: job.ID, Status: "cancelled", Signal: "none",
		})
	}
	manifest, managedErr := managedJobRecord(*job)
	managed := managedErr == nil
	if managedErr != nil && !os.IsNotExist(managedErr) {
		return api.Failure(id, api.ErrJobStore, managedErr.Error())
	}
	if managed {
		return s.runManagedCancel(id, store, *job, manifest)
	}
	if err := syscall.Kill(pgid, 0); err == syscall.ESRCH {
		// Runner already gone → the group may still have orphans;
		// kill(-pgid, 0) decides whether anything remains.
		if !processGroupAlive(pgid) {
			s.recordCancellation(store, *job, true)
			s.invalidateJobCache()
			return api.Success(id, api.RunCancelledResult{
				Type: "run_cancelled", Job: job.ID, Status: "cancelled", Signal: "none",
			})
		}
		return api.Failure(id, api.ErrJobStore,
			fmt.Sprintf("job %s runner pid %d is gone but process group %d still lives; refusing to signal", job.ID, pgid, pgid))
	}
	// SPEC: verify the runner pid is the leader of a process group of the
	// same number before acquiring 'cancelling' and signalling.
	if g, err := syscall.Getpgid(pgid); err != nil || g != pgid {
		return api.Failure(id, api.ErrJobStore,
			fmt.Sprintf("job %s runner pid %d is not a process group leader; refusing to signal", job.ID, pgid))
	}
	identity, err := jobProcessIdentity(pgid)
	if err != nil {
		return api.Failure(id, api.ErrJobStore, "process start identity unavailable; refusing to signal: "+err.Error())
	}
	ok, err := store.MarkCommandCancelling(job.ID)
	if err != nil {
		return api.Failure(id, api.ErrJobStore, err.Error())
	}
	if !ok {
		return api.Failure(id, api.ErrJobNotFound,
			fmt.Sprintf("job %s is not running (status=%s)", job.ID, job.Status))
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	s.invalidateJobCache()
	s.startJobMonitor(job.ID, func(stop <-chan struct{}) error {
		s.finishCancel(job.ID, jobRunnerRecord{PID: pgid, Identity: identity}, stop)
		return nil
	})
	return api.Success(id, api.RunCancelledResult{
		Type: "run_cancelled", Job: job.ID, Status: "cancelling", Signal: "TERM",
	})
}

// processGroupAlive reports whether any process still runs in group pgid
// (kill(-pgid, 0): ESRCH means gone; EPERM/nil means someone is alive).
func processGroupAlive(pgid int) bool {
	err := syscall.Kill(-pgid, 0)
	if err != nil && err != syscall.EPERM {
		return false
	}
	return processGroupHasLiveMembers(pgid)
}

// finishCancel is the cancel monitor: after the initial SIGTERM it gives
// the group 2 s, escalates to SIGKILL, and only then — once the whole
// group is confirmed gone — marks the job 'cancelled'. If the group
// outlives SIGKILL the row stays 'cancelling' (fail closed, SPEC G9).
func (s *Server) finishCancel(jobID string, manifest jobRunnerRecord, stop <-chan struct{}) {
	pgid := manifest.PID
	if waitJobGroupGone(pgid, 2*time.Second, stop) {
		s.cancelIfCancelling(jobID)
		return
	}
	if jobMonitorStopped(stop) {
		return
	}
	state := jobCancelState{VerifiedPGID: pgid, VerifiedIdentity: manifest.Identity}
	if err := signalCancellationGroup(manifest, state, syscall.SIGKILL); err != nil {
		fmt.Fprintf(os.Stderr, "ikesu job cancellation %s: %v; refusing to signal\n", jobID, err)
		return
	}
	if waitJobGroupGone(pgid, 30*time.Second, stop) {
		s.cancelIfCancelling(jobID)
	}
	// else: leave 'cancelling' — better to keep polling state honest than
	// to claim a process group is dead when it is not.
}

// cancelIfCancelling records 'cancelled' only while the row is still in
// the cancelling branch (it may already be 'exited' or cancelled by a
// concurrent monitor — either terminal is fine).
func (s *Server) cancelIfCancelling(jobID string) {
	store, err := s.db()
	if err != nil {
		return
	}
	if j, err := store.CommandRow(jobID); err == nil && j != nil && j.Status == "cancelling" {
		if err := s.recordCancellation(store, *j, false); err != nil {
			fmt.Fprintf(os.Stderr, "ikesu job cancellation: %v\n", err)
		}
	}
}
func (s *Server) recordCancellation(store *dispatch.Store, j dispatch.Job, onLoop bool) error {
	reason := ""
	if manifest, err := managedJobRecord(j); err == nil {
		if processGroupAlive(manifest.PID) {
			return nil
		}
		// The helper drains and closes the log before publishing this result.
		if _, err := completedJobRecord(j, manifest); err != nil {
			gone, probeErr := jobMonitorGone(manifest)
			if probeErr != nil {
				return probeErr
			}
			if !gone {
				return nil
			}
			reason = "completion unavailable: runner monitor disappeared: " + err.Error()
		}
	}
	body := ""
	if j.Completion != "none" {
		body = fmt.Sprintf("[ikesu run] cancelled label=%s job=%s details: ikesu log %s", labelOrDash(j.Label), j.ID, j.ID)
		if reason != "" {
			body += " reason: " + reason
		}
	}
	changed, err := store.CompleteJob(j.ID, j.Status, "cancelled", nil, time.Now().UnixMilli(), j.CallerPane, body)
	if err != nil {
		return err
	}
	if changed && reason != "" {
		fmt.Fprintf(os.Stderr, "ikesu job %s: %s\n", j.ID, reason)
		if f, e := openJobLog(j.LogPath); e == nil {
			_, _ = fmt.Fprintln(f, "\ncancellation_error: "+reason)
			_ = f.Close()
		}
	}
	if changed && body != "" {
		if onLoop {
			s.flushJobNudgeFor(dispatch.JobsRoom, j.CallerPane)
		} else {
			s.app.Do(func() { s.flushJobNudgeFor(dispatch.JobsRoom, j.CallerPane) })
		}
	}
	return nil
}

func openJobLog(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
}

// readLogTail returns the last n lines of a job log ("" on error).
func readLogTail(path string, n int) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// abortOrphanedJobs marks jobs still "running" from a previous server
// instance as cancelled — the old runner cannot report back.
func (s *Server) abortOrphanedJobs() {
	store, err := s.db()
	if err != nil {
		return
	}
	jobs, err := store.CommandRows(0)
	if err != nil {
		return
	}
	for _, j := range jobs {
		if j.Status == "running" || j.Status == "cancelling" {
			if _, err := managedJobRecord(j); err == nil {
				continue
			}
			_, _ = store.MarkCommandCancelled(j.ID)
		}
	}
}

// jobSnapshot lists jobs newest-first and probes runner liveness once —
// the SPEC G2 workspace-dot snapshot. alive maps job id → runner alive;
// callers must not re-check processes while rendering. When the DB file
// has never been created there are no jobs; skip opening the store so
// the once-per-second refresh does not load the engine into the server.
func (s *Server) jobSnapshot() ([]dispatch.Job, map[string]bool) {
	if !dispatch.DBExists(s.session) {
		return nil, nil
	}
	store, err := s.db()
	if err != nil {
		return nil, nil
	}
	jobs, err := store.CommandRows(0)
	if err != nil {
		return nil, nil
	}
	alive := map[string]bool{}
	for _, j := range jobs {
		if (j.Status == "running" || j.Status == "cancelling") && j.RunnerPID != nil {
			alive[j.ID] = processGroupAlive(int(*j.RunnerPID))
		}
	}
	return jobs, alive
}

func (s *Server) queueJobNotice(store *dispatch.Store, caller, body string, onLoop bool) {
	if !dispatch.IsGlobalPaneID(caller) {
		_ = store.InsertFailedMessage(dispatch.JobsRoom, "", dispatch.RunShellActor, caller, body, "job notification has no immutable caller pane", nil)
		return
	}
	if _, err := store.InsertMessage(dispatch.JobsRoom, "", dispatch.RunShellActor, caller, body, nil, false); err != nil {
		return
	}
	if onLoop {
		s.flushJobNudgeFor(dispatch.JobsRoom, caller)
	} else {
		s.app.Do(func() { s.flushJobNudgeFor(dispatch.JobsRoom, caller) })
	}
}
