package server

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

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

func jobLogPath(jobID, label string) string {
	return dispatch.JobLogPath(jobID, label)
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
	// The completion nudge is addressed to the caller pane's mailbox; the
	// pane id works for unlabeled panes.
	callerAgent := ""
	callerPane := ""
	if p.CallerPane != "" {
		t, err := s.resolveTarget(p.CallerPane, false)
		if err != nil {
			return api.Failure(id, err.Code, err.Message)
		}
		callerPane = fmtPaneID(t.pid)
		callerAgent, _ = s.mailboxName(t.pid)
		if callerAgent == "" {
			callerAgent = callerPane
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
	logPath := jobLogPath(jobID, label)
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
	cmd := exec.CommandContext(ctx, p.Argv[0], p.Argv[1:]...)
	cmd.Dir = cwd
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		cancel()
		logFile.Close()
		s.finishJob(store, jobID, nil)
		return api.Failure(id, api.ErrJobStore, err.Error())
	}
	s.jobProcs.put(jobID, cmd)
	if err := store.MarkCommandRunning(jobID, int64(cmd.Process.Pid)); err != nil {
		s.jobProcs.drop(jobID)
		cancel()
		logFile.Close()
		_ = cmd.Process.Kill()
		return api.Failure(id, api.ErrJobStore, err.Error())
	}
	go s.watchJob(jobID, cmd, cancel, logFile, callerAgent, label, completion)

	return api.Success(id, api.RunStartedResult{
		Type: "run_started", Job: jobID, Label: label, Mode: "background",
	})
}

// finishJob records the terminal state for a job that never ran; the
// row is still 'queued', so a plain finish is a no-op and it ends as
// 'cancelled'.
func (s *Server) finishJob(store *dispatch.Store, jobID string, exitCode *int64) {
	if ok, _ := store.MarkCommandFinished(jobID, exitCode); !ok {
		_, _ = store.MarkCommandCancelled(jobID)
	}
}

// watchJob waits for the process, persists the terminal state, and posts
// the completion nudge.
func (s *Server) watchJob(jobID string, cmd *exec.Cmd, cancel context.CancelFunc,
	logFile *os.File, callerAgent, label, completion string) {
	_ = cmd.Wait()
	logFile.Close()
	cancel()
	s.jobProcs.drop(jobID)

	store, derr := s.db()
	if derr != nil {
		return
	}
	var exitCode int64 = -1
	if cmd.ProcessState != nil {
		exitCode = int64(cmd.ProcessState.ExitCode())
	}
	// 'running' → 'exited' with the real code. When run.cancel already
	// moved the row to 'cancelling', finish is a no-op and the terminal
	// status is 'cancelled' — so finish must be tried first.
	if ok, _ := store.MarkCommandFinished(jobID, &exitCode); !ok {
		_, _ = store.MarkCommandCancelled(jobID)
	}

	if callerAgent == "" || completion == "none" {
		return
	}
	body := fmt.Sprintf("[gtx run] exit=%d label=%s job=%s details: gtx log %s",
		exitCode, labelOrDash(label), jobID, jobID)
	if completion == "full" {
		if tail := readLogTail(jobLogPathFrom(store, jobID), 40); tail != "" {
			body += "\n" + tail
		}
	}
	if _, ierr := store.InsertMessage(dispatch.JobsRoom, "",
		dispatch.RunShellActor, callerAgent, body, nil, false); ierr != nil {
		return
	}
	s.app.Do(func() { s.flushJobNudgeFor(dispatch.JobsRoom, callerAgent) })
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

// runCancel handles run.cancel: move the row to 'cancelling' and signal
// the process group; the watcher records the terminal state.
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
	ok, err := store.MarkCommandCancelling(job.ID)
	if err != nil {
		return api.Failure(id, api.ErrJobStore, err.Error())
	}
	if !ok {
		return api.Failure(id, api.ErrJobNotFound,
			fmt.Sprintf("job %s is not running (status=%s)", job.ID, job.Status))
	}
	if cmd := s.jobProcs.get(job.ID); cmd != nil && cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	} else if job.RunnerPID != nil {
		_ = syscall.Kill(int(-*job.RunnerPID), syscall.SIGTERM)
	}
	return api.Success(id, api.RunCancelledResult{
		Type: "run_cancelled", Job: job.ID, Status: "cancelled", Signal: "TERM",
	})
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
