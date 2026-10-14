package server

import (
	"context"
	"fmt"
	"github.com/kazuph/goterrace/internal/childenv"
	"github.com/kazuph/goterrace/internal/machine"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/dispatch"
)

// Pane-less background jobs (gtx run). A job is a command dispatch row in
// the SQLite store plus a detached child process whose output streams to a
// log file under StateDir()/job-logs. Completion writes one nudge into the
// caller's jobs-room mailbox; delivery follows the regular mailbox
// pipeline.

// jobProcs tracks live jobs (external job id → process) so run.cancel can
// signal the process group.
type jobProcsT struct {
	mu  sync.Mutex
	m   map[string]*exec.Cmd
	seq int // next external job number, seeded from the store
}

func (j *jobProcsT) nextID() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.seq++
	return fmt.Sprintf("j%04d", j.seq)
}

func (j *jobProcsT) put(id string, cmd *exec.Cmd) {
	j.mu.Lock()
	j.m[id] = cmd
	j.mu.Unlock()
}

func (j *jobProcsT) get(id string) *exec.Cmd {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.m[id]
}

func (j *jobProcsT) drop(id string) {
	j.mu.Lock()
	delete(j.m, id)
	j.mu.Unlock()
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
// and hand completion to a watcher goroutine.
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

	logFile, err := openJobLog(logPath)
	if err != nil {
		s.finishJob(store, jobID, nil)
		return api.Failure(id, api.ErrJobStore, err.Error())
	}
	ctx, cancel := context.WithCancel(context.Background())
	profiles := (machine.Catalog{Path: filepath.Join(s.dir, "machines.json")}).Load()
	var cmd *exec.Cmd
	approval, argv := machine.PrepareRunApproval(profiles, p.Argv, func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		cancel()
	})
	var progress *machine.SyncProgress
	if approval != nil {
		argv, progress, err = approval.SyncProgress(argv)
		if err != nil {
			cancel()
			logFile.Close()
			s.finishJob(store, jobID, nil)
			return api.Failure(id, api.ErrJobStore, err.Error())
		}
	}
	cmd = childenv.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = childenv.Scrub(os.Environ())
	cmd.Dir = cwd
	var logMu sync.Mutex
	logMu.Lock()
	cmd.Stdout = &jobLogStream{out: logFile, mu: &logMu, prefix: "[stdout] ", lineStart: true}
	cmd.Stderr = &jobLogStream{out: logFile, mu: &logMu, prefix: "[stderr] ", lineStart: true}
	var approvalOut, approvalErr *machine.ApprovalStream
	if approval != nil {
		approvalOut = &machine.ApprovalStream{State: approval, Out: cmd.Stdout}
		approvalErr = &machine.ApprovalStream{State: approval, Out: cmd.Stderr}
		cmd.Stdout, cmd.Stderr = approvalOut, approvalErr
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if progress != nil {
		cmd.ExtraFiles = progress.Child
	}
	if err := cmd.Start(); err != nil {
		if progress != nil {
			_ = progress.Close()
		}
		logMu.Unlock()
		cancel()
		logFile.Close()
		s.finishJob(store, jobID, nil)
		return api.Failure(id, api.ErrJobStore, err.Error())
	}
	if progress != nil {
		progress.Started(approvalErr)
	}
	s.jobProcs.put(jobID, cmd)
	if err := store.MarkCommandRunning(jobID, int64(cmd.Process.Pid)); err != nil {
		logMu.Unlock()
		s.jobProcs.drop(jobID)
		cancel()
		logFile.Close()
		_ = cmd.Process.Kill()
		return api.Failure(id, api.ErrJobStore, err.Error())
	}
	row, rowErr := store.CommandRow(jobID)
	if rowErr != nil || row == nil || row.StartedMs == nil {
		logMu.Unlock()
		cancel()
		_ = cmd.Wait()
		_ = logFile.Close()
		s.jobProcs.drop(jobID)
		s.finishJob(store, jobID, nil)
		return api.Failure(id, api.ErrJobStore, "started job timestamp unavailable")
	}
	quoted := make([]string, len(p.Argv))
	for i, arg := range p.Argv {
		quoted[i] = "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
	}
	_, headerErr := fmt.Fprintf(logFile, "job_id: %s\nrunner_pid: %d\ncommand: %s\ncwd: %s\nstarted_unix_ms: %d\n\n",
		jobID, cmd.Process.Pid, strings.Join(quoted, " "), cwd, *row.StartedMs)
	logMu.Unlock()
	if headerErr != nil {
		cancel()
		_ = cmd.Wait()
		_ = logFile.Close()
		s.jobProcs.drop(jobID)
		s.finishJob(store, jobID, nil)
		return api.Failure(id, api.ErrJobStore, headerErr.Error())
	}
	go s.watchJob(jobID, cmd, cancel, logFile, callerPane, label, completion, approval, approvalOut, approvalErr, progress)
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

// watchJob waits for the process, persists the terminal state, and posts
// the completion nudge.
func (s *Server) watchJob(jobID string, cmd *exec.Cmd, cancel context.CancelFunc,
	logFile *os.File, callerAgent, label, completion string, approval *machine.RunApproval, approvalOut, approvalErr *machine.ApprovalStream, progress *machine.SyncProgress) {
	defer s.invalidateJobCache()
	_ = cmd.Wait()
	if progress != nil {
		_ = progress.Close()
	}
	if approval != nil {
		_ = approvalOut.Close()
		_ = approvalErr.Close()
		approval.Close()
	}
	exit := -1
	if cmd.ProcessState != nil {
		exit = cmd.ProcessState.ExitCode()
	}
	store, derr := s.db()
	writeFinish := true
	if derr == nil {
		if j, err := store.CommandRow(jobID); err == nil && j != nil {
			writeFinish = j.Status != "cancelling" && j.Status != "cancelled"
		}
	}
	if writeFinish {
		_, _ = fmt.Fprintf(logFile, "\nfinished_unix_ms: %d\nexit_code: %d\n", time.Now().UnixMilli(), exit)
	}
	logFile.Close()
	cancel()
	s.jobProcs.drop(jobID)

	if derr != nil {
		return
	}
	var exitCode int64 = -1
	if cmd.ProcessState != nil {
		exitCode = int64(cmd.ProcessState.ExitCode())
	}
	// 'running' → 'exited' with the real code. When run.cancel already
	// moved the row to 'cancelling', finish is a no-op — and the row must
	// NOT go 'cancelled' here: SPEC G9 requires the whole process group to
	// be gone first, which only the cancel monitor (finishCancel) checks.
	if ok, _ := store.MarkCommandFinished(jobID, &exitCode); !ok {
		if j, _ := store.CommandRow(jobID); j == nil || j.Status != "cancelling" {
			_, _ = store.MarkCommandCancelled(jobID)
		}
		return // cancellation owns its notice; never also report an exit
	}

	if !writeFinish || completion == "none" {
		return
	}
	body := fmt.Sprintf("[gtx run] exit=%d label=%s job=%s details: gtx log %s",
		exitCode, labelOrDash(label), jobID, jobID)
	if completion == "full" {
		if tail := readLogTail(jobLogPathFrom(store, jobID), 40); tail != "" {
			body += "\n" + tail
		}
	}
	s.queueJobNotice(store, callerAgent, body, false)
}

func labelOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func jobLogPathFrom(store *dispatch.Store, jobID string) string {
	j, err := store.CommandRow(jobID)
	if err != nil || j == nil {
		return ""
	}
	return j.LogPath
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
	// Resolve the runner pid: the live process map first, else the pid
	// persisted in the row (survives server restarts).
	pgid := 0
	if cmd := s.jobProcs.get(job.ID); cmd != nil && cmd.Process != nil {
		pgid = cmd.Process.Pid
	} else if job.RunnerPID != nil {
		pgid = int(*job.RunnerPID)
	}
	if pgid <= 0 {
		// No runner pid recorded — nothing left to signal.
		s.recordCancellation(store, *job, true)
		return api.Success(id, api.RunCancelledResult{
			Type: "run_cancelled", Job: job.ID, Status: "cancelled", Signal: "none",
		})
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
	go s.finishCancel(job.ID, pgid)
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
func (s *Server) finishCancel(jobID string, pgid int) {
	if waitProcessGroupGone(pgid, 2*time.Second) {
		s.cancelIfCancelling(jobID)
		return
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	if waitProcessGroupGone(pgid, 30*time.Second) {
		s.cancelIfCancelling(jobID)
	}
	// else: leave 'cancelling' — better to keep polling state honest than
	// to claim a process group is dead when it is not.
}

func waitProcessGroupGone(pgid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processGroupAlive(pgid) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return !processGroupAlive(pgid)
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
		s.recordCancellation(store, *j, false)
	}
}
func (s *Server) recordCancellation(store *dispatch.Store, j dispatch.Job, onLoop bool) {
	if changed, _ := store.MarkCommandCancelled(j.ID); changed && j.Completion != "none" {
		body := fmt.Sprintf("[gtx run] cancelled label=%s job=%s details: gtx log %s", labelOrDash(j.Label), j.ID, j.ID)
		s.queueJobNotice(store, j.CallerPane, body, onLoop)
	}
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
