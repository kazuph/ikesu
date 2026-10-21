package server

import (
	"context"
	"crypto/rand"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/childenv"
	"github.com/kazuph/goterrace/internal/dispatch"
	"github.com/kazuph/goterrace/internal/machine"
	"golang.org/x/sys/unix"
)

const jobRunnerArg = "__job-runner"

type jobRunnerRequest struct {
	JobID, LogPath, Cwd, ProfilesPath, Token string
	Namespace                                string
	Argv                                     []string
}
type jobRunnerStarted struct {
	PID             int
	Identity, Error string
}
type jobRunnerCommit struct{ StartedMs int64 }
type jobRunnerRecord struct {
	JobID, Token, Identity string
	PID                    int
	MonitorPID             int
	MonitorIdentity        string
	StartedMs, FinishedMs  int64
	ExitCode               int64
	Error                  string
}

func writeJobRecord(path string, record any) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".job-result-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func readJobRecord(path string) (jobRunnerRecord, error) {
	var record jobRunnerRecord
	data, err := os.ReadFile(path)
	if err != nil {
		return record, err
	}
	err = json.Unmarshal(data, &record)
	return record, err
}

// RunJobRunner is an internal child entry point, also used by the real-process
// server tests. Descriptor 3 is a private startup handshake, never user input.
func RunJobRunner() (int, error) {
	f := os.NewFile(3, "job-startup")
	conn, err := net.FileConn(f)
	f.Close()
	if err != nil {
		return 1, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(handoffWait))
	enc, dec := gob.NewEncoder(conn), gob.NewDecoder(conn)
	var request jobRunnerRequest
	if err = dec.Decode(&request); err != nil {
		return 1, err
	}
	// os.Executable may resolve a versioned target behind a gtx symlink.
	// Approval files must retain the calling server's namespace.
	api.ConfigureExecutable(request.Namespace)
	fail := func(err error) (int, error) { _ = enc.Encode(jobRunnerStarted{Error: err.Error()}); return 1, err }
	if len(request.Argv) == 0 {
		return fail(errors.New("empty runner argv"))
	}
	logFile, err := openJobLog(request.LogPath)
	if err != nil {
		return fail(err)
	}
	defer logFile.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var cmd *exec.Cmd
	profiles := (machine.Catalog{Path: request.ProfilesPath}).Load()
	approval, argv := machine.PrepareRunApproval(profiles, request.Argv, func() {
		if cmd != nil && cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		cancel()
	})
	var progress *machine.SyncProgress
	if approval != nil {
		argv, progress, err = approval.SyncProgress(argv)
		if err != nil {
			return fail(err)
		}
		defer approval.Close()
	}
	cmd = childenv.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = request.Cwd
	var logMu sync.Mutex
	logMu.Lock()
	stdout := &jobLogStream{out: logFile, mu: &logMu, prefix: "[stdout] ", lineStart: true}
	stderr := &jobLogStream{out: logFile, mu: &logMu, prefix: "[stderr] ", lineStart: true}
	cmd.Stdout, cmd.Stderr = stdout, stderr
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
	if err = cmd.Start(); err != nil {
		logMu.Unlock()
		if progress != nil {
			_ = progress.Close()
		}
		return fail(err)
	}
	if progress != nil {
		progress.Started(approvalErr)
	}
	abort := func() {
		logMu.Unlock()
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		cancel()
		_ = cmd.Wait()
		if progress != nil {
			_ = progress.Close()
		}
	}
	identity, err := jobProcessIdentity(cmd.Process.Pid)
	if err != nil {
		abort()
		return fail(err)
	}
	if err = enc.Encode(jobRunnerStarted{PID: cmd.Process.Pid, Identity: identity}); err != nil {
		abort()
		return 1, err
	}
	var commit jobRunnerCommit
	if err = dec.Decode(&commit); err != nil {
		abort()
		return 1, err
	}
	record := jobRunnerRecord{JobID: request.JobID, PID: cmd.Process.Pid, Identity: identity, Token: request.Token, StartedMs: commit.StartedMs}
	record.MonitorPID = os.Getpid()
	record.MonitorIdentity, err = jobProcessIdentity(record.MonitorPID)
	if err != nil {
		abort()
		return fail(err)
	}
	quoted := make([]string, len(request.Argv))
	for i, arg := range request.Argv {
		quoted[i] = "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
	}
	_, err = fmt.Fprintf(logFile, "job_id: %s\nrunner_pid: %d\ncommand: %s\ncwd: %s\nstarted_unix_ms: %d\n\n", request.JobID, cmd.Process.Pid, strings.Join(quoted, " "), request.Cwd, commit.StartedMs)
	if err == nil {
		err = writeJobRecord(request.LogPath+".runner", record)
	}
	if err != nil {
		abort()
		return fail(err)
	}
	if err = enc.Encode(jobRunnerStarted{PID: cmd.Process.Pid, Identity: identity}); err != nil {
		abort()
		return 1, err
	}
	conn.Close()
	logMu.Unlock()
	waitErr := cmd.Wait()
	if progress != nil {
		if e := progress.Close(); e != nil {
			record.Error = e.Error()
		}
	}
	if approval != nil {
		if e := approvalOut.Close(); e != nil {
			record.Error = e.Error()
		}
		if e := approvalErr.Close(); e != nil {
			record.Error = e.Error()
		}
	}
	if e := stdout.Error(); e != nil {
		record.Error = e.Error()
	}
	if e := stderr.Error(); e != nil {
		record.Error = e.Error()
	}
	record.FinishedMs = time.Now().UnixMilli()
	record.ExitCode = int64(cmd.ProcessState.ExitCode())
	var exitErr *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exitErr) {
		record.Error = waitErr.Error()
	}
	var cancellation jobCancelState
	cancelData, cancelErr := os.ReadFile(request.LogPath + ".cancel")
	cancelled := cancelErr == nil && json.Unmarshal(cancelData, &cancellation) == nil && cancellation.TermSentMs > 0
	if !cancelled {
		if _, err = fmt.Fprintf(logFile, "\nfinished_unix_ms: %d\nexit_code: %d\n", record.FinishedMs, record.ExitCode); err != nil {
			record.Error = err.Error()
		}
	}
	if err = logFile.Sync(); err != nil {
		record.Error = err.Error()
	}
	if err = logFile.Close(); err != nil {
		record.Error = err.Error()
	}
	if err = writeJobRecord(request.LogPath+".result", record); err != nil {
		return 1, err
	}
	return 0, nil
}

func (s *Server) startJobRunner(store *dispatch.Store, jobID, logPath, cwd string, argv []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	syscall.ForkLock.RLock()
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err == nil {
		unix.CloseOnExec(pair[0])
		unix.CloseOnExec(pair[1])
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		return err
	}
	parent, child := os.NewFile(uintptr(pair[0]), "job-parent"), os.NewFile(uintptr(pair[1]), "job-child")
	defer child.Close()
	conn, err := net.FileConn(parent)
	parent.Close()
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(handoffWait))
	cmd := childenv.Command(exe, jobRunnerArg)
	cmd.ExtraFiles = []*os.File{child}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err = cmd.Start(); err != nil {
		return err
	}
	child.Close()
	// Reaping the helper does not own completion; its durable result does.
	go func() { _ = cmd.Wait(); s.invalidateJobCache() }()
	enc, dec := gob.NewEncoder(conn), gob.NewDecoder(conn)
	request := jobRunnerRequest{JobID: jobID, LogPath: logPath, Cwd: cwd, ProfilesPath: filepath.Join(s.dir, "machines.json"), Argv: argv, Token: rand.Text(), Namespace: api.BinaryNamespace()}
	if err = enc.Encode(request); err != nil {
		return err
	}
	var started jobRunnerStarted
	if err = dec.Decode(&started); err != nil {
		return err
	}
	if started.Error != "" {
		return errors.New(started.Error)
	}
	if err = store.MarkCommandRunning(jobID, int64(started.PID)); err != nil {
		return err
	}
	row, err := store.CommandRow(jobID)
	if err != nil {
		return err
	}
	if row == nil || row.StartedMs == nil {
		return errors.New("started job timestamp unavailable")
	}
	if err = enc.Encode(jobRunnerCommit{StartedMs: *row.StartedMs}); err != nil {
		return err
	}
	if err = dec.Decode(&started); err != nil {
		return err
	}
	if started.Error != "" {
		return errors.New(started.Error)
	}
	return nil
}

func managedJobRecord(job dispatch.Job) (jobRunnerRecord, error) {
	record, err := readJobRecord(job.LogPath + ".runner")
	if err != nil {
		return record, err
	}
	if job.RunnerPID == nil || job.StartedMs == nil || record.JobID != job.ID || record.PID != int(*job.RunnerPID) || record.StartedMs != *job.StartedMs || record.Token == "" || record.Identity == "" || record.MonitorPID <= 0 || record.MonitorIdentity == "" {
		return record, fmt.Errorf("job %s runner identity mismatch", job.ID)
	}
	return record, nil
}

func completedJobRecord(job dispatch.Job, manifest jobRunnerRecord) (jobRunnerRecord, error) {
	result, err := readJobRecord(job.LogPath + ".result")
	if err != nil {
		return result, err
	}
	if result.JobID != manifest.JobID || result.Token != manifest.Token || result.PID != manifest.PID || result.StartedMs != manifest.StartedMs || result.Identity != manifest.Identity || result.FinishedMs < manifest.StartedMs {
		return result, fmt.Errorf("job %s completion identity mismatch", job.ID)
	}
	if result.Error != "" {
		return result, fmt.Errorf("job %s runner: %s", job.ID, result.Error)
	}
	return result, nil
}

func (s *Server) reconcileJobResults(store *dispatch.Store, jobs []dispatch.Job) error {
	var failures []error
	for _, job := range jobs {
		if job.Status != "running" && job.Status != "cancelling" {
			continue
		}
		manifest, err := managedJobRecord(job)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if job.Status == "cancelling" {
			s.resumeJobCancel(job.ID, manifest)
			continue
		}
		result, err := completedJobRecord(job, manifest)
		if os.IsNotExist(err) {
			identity, identityErr := jobProcessIdentity(manifest.MonitorPID)
			if identityErr != nil || identity != manifest.MonitorIdentity {
				// The result may have appeared between the first read and exit.
				if _, e := os.Stat(job.LogPath + ".result"); os.IsNotExist(e) {
					failures = append(failures, fmt.Errorf("job %s monitor disappeared without a completion result", job.ID))
				}
			}
			continue
		}
		if err != nil {
			failures = append(failures, err)
			continue
		}
		body := ""
		if job.Completion != "none" {
			body = fmt.Sprintf("[gtx run] exit=%d label=%s job=%s details: gtx log %s", result.ExitCode, labelOrDash(job.Label), job.ID, job.ID)
			if job.Completion == "full" {
				if tail := readLogTail(job.LogPath, 40); tail != "" {
					body += "\n" + tail
				}
			}
		}
		changed, err := store.CompleteJob(job.ID, "running", "exited", &result.ExitCode, result.FinishedMs, job.CallerPane, body)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if changed && body != "" {
			s.app.Do(func() { s.flushJobNudgeFor(dispatch.JobsRoom, job.CallerPane) })
		}
	}
	return errors.Join(failures...)
}
