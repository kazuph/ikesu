package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/dispatch"
	sqlite3 "github.com/ncruces/go-sqlite3"
)

type faultJob struct {
	s            *session
	store        *dispatch.Store
	job          *dispatch.Job
	release      *os.File
	sink, dbPath string
}

func faultSQL(t *testing.T, path, sql string) {
	t.Helper()
	c, err := sqlite3.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err = c.Exec(sql); err != nil {
		t.Fatal(err)
	}
}

func startFaultJob(t *testing.T) *faultJob {
	return startFaultJobScript(t, func(fifo string) string {
		return fmt.Sprintf("trap '' TERM; printf 'READY\\n'; read value < %q; printf 'FINAL_OUTPUT\\n'; exit 7", fifo)
	})
}

func startFaultJobScript(t *testing.T, script func(string) string) *faultJob {
	return startFaultJobScriptCompletion(t, script, "summary")
}

func startFaultJobScriptCompletion(t *testing.T, script func(string) string, completion string) *faultJob {
	t.Helper()
	s := start(t)
	s.waitFor("shell", 15*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	sink := filepath.Join(t.TempDir(), "received")
	s.send(fmt.Sprintf("stty -echo; cat >> %q\r", sink))
	s.waitFor("real notice reader", 5*time.Second, func(string) bool { _, err := os.Stat(sink); return err == nil })
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "fault-job", Agent: "codex", State: "idle"}, nil)
	fifo := filepath.Join(t.TempDir(), "release")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	release, err := os.OpenFile(fifo, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { release.Close() })
	var started api.RunStartedResult
	s.rpc(api.MethodRunStart, api.RunStartParams{CallerPane: "p1", Label: "fault-job", Completion: completion, Argv: []string{"/bin/sh", "-c", script(fifo)}}, &started)
	dbPath := filepath.Join(s.stateDir, "sessions", s.name, "ikesu.db")
	store, err := dispatch.OpenReadOnly(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	job, err := store.CommandRow(started.Job)
	if err != nil || job == nil || job.RunnerPID == nil {
		t.Fatal(job, err)
	}
	t.Cleanup(func() {
		if syscall.Kill(int(*job.RunnerPID), 0) == nil {
			_ = syscall.Kill(-int(*job.RunnerPID), syscall.SIGKILL)
		}
	})
	s.waitFor("job ready", 5*time.Second, func(string) bool {
		data, _ := os.ReadFile(job.LogPath)
		return strings.Contains(string(data), "[stdout] READY\n")
	})
	return &faultJob{s: s, store: store, job: job, release: release, sink: sink, dbPath: dbPath}
}

func TestCancelReapedLeaderWithSurvivingChild(t *testing.T) {
	t.Parallel()
	for _, handoff := range []bool{false, true} {
		t.Run(fmt.Sprintf("handoff=%t", handoff), func(t *testing.T) {
			childFile := filepath.Join(t.TempDir(), "child-pid")
			j := startFaultJobScript(t, func(fifo string) string {
				return fmt.Sprintf("/bin/sh -c 'trap \"\" TERM; printf \"READY\\n\"; read value < \"$1\"' child %q & echo $! > %q; wait", fifo, childFile)
			})
			pid := int(*j.job.RunnerPID)
			t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
			data, err := os.ReadFile(childFile)
			if err != nil {
				t.Fatal(err)
			}
			child := strings.TrimSpace(string(data))
			if out, err := j.s.cli("run", "cancel", j.job.ID); err != nil {
				t.Fatal(out, err)
			}
			j.s.waitFor("leader reaped while child holds stdout", 5*time.Second, func(string) bool { return syscall.Kill(pid, 0) == syscall.ESRCH })
			state, err := exec.Command("ps", "-o", "stat=", "-p", child).Output()
			if err != nil || strings.HasPrefix(strings.TrimSpace(string(state)), "Z") {
				t.Fatal("child did not survive TERM", string(state), err)
			}
			if _, err := os.Stat(j.job.LogPath + ".result"); !os.IsNotExist(err) {
				t.Fatal("stdout pipe was not held by child", err)
			}
			if handoff {
				if out, err := j.s.cli("server", "live-handoff", "--import-exe", binPath); err != nil {
					t.Fatal(out, err)
				}
			}
			j.s.waitFor("remaining child killed and cancellation delivered once", 10*time.Second, func(string) bool {
				row, _ := j.store.CommandRow(j.job.ID)
				return row != nil && row.Status == "cancelled" && row.ExitCode == nil && j.notices() == 1
			})
			state, err = exec.Command("ps", "-o", "stat=", "-p", child).Output()
			if err == nil && !strings.HasPrefix(strings.TrimSpace(string(state)), "Z") {
				t.Fatal("child still executes after cancellation", string(state))
			}
			if out, err := j.s.cli("server", "live-handoff", "--import-exe", binPath); err != nil {
				t.Fatal(out, err)
			}
			if j.notices() != 1 {
				t.Fatal("duplicate cancellation notice", j.notices())
			}
			t.Log("leader reaped, child survived TERM and retained pipe, then KILL completed cancellation with nil exit and one real PTY notice")
		})
	}
}

func (j *faultJob) notices() int {
	data, _ := os.ReadFile(j.sink)
	return strings.Count(string(data), "job="+j.job.ID+" ")
}

func TestJobNoticeAcknowledgementFailureHandoff(t *testing.T) {
	t.Parallel()
	j := startFaultJob(t)
	faultSQL(t, j.dbPath, `CREATE TRIGGER reject_delivery BEFORE UPDATE OF delivered_at ON dispatches WHEN OLD.kind='message' BEGIN SELECT RAISE(ABORT,'delivery rejected'); END`)
	if _, err := j.release.WriteString("go\n"); err != nil {
		t.Fatal(err)
	}
	j.s.waitFor("notice written before acknowledgement", 10*time.Second, func(string) bool { return j.notices() == 1 })
	if out, err := j.s.cli("server", "live-handoff", "--import-exe", binPath); err == nil || !strings.Contains(out, "acknowledgement") {
		t.Fatal("handoff committed with an unacknowledged PTY write", out, err)
	}
	if j.notices() != 1 {
		t.Fatal("notice repeated before recovery", j.notices())
	}
	faultSQL(t, j.dbPath, `DROP TRIGGER reject_delivery`)
	if out, err := j.s.cli("server", "live-handoff", "--import-exe", binPath); err != nil {
		t.Fatal(out, err)
	}
	j.s.waitFor("notice acknowledgement recovered", 5*time.Second, func(string) bool {
		pending, err := j.store.PendingJobMessages()
		return err == nil && len(pending) == 0
	})
	if j.notices() != 1 {
		t.Fatal("notice repeated after recovery/handoff", j.notices())
	}
	t.Log("real PTY written once; UPDATE failure blocked handoff; recovery acknowledged the same IDs without rewriting")
}

func TestManagedJobResultSurvivesRetentionAndDBFailure(t *testing.T) {
	t.Parallel()
	j := startFaultJob(t)
	faultSQL(t, j.dbPath, fmt.Sprintf("UPDATE dispatches SET created_at='%s' WHERE kind='command' AND external_id='%s'; CREATE TRIGGER reject_finish BEFORE UPDATE OF status ON dispatches WHEN OLD.kind='command' AND NEW.status='exited' BEGIN SELECT RAISE(ABORT,'finish rejected'); END", time.Now().Add(-dispatch.JobRetention-time.Hour).UTC().Format("2006-01-02T15:04:05.000Z"), j.job.ID))
	if _, err := j.release.WriteString("go\n"); err != nil {
		t.Fatal(err)
	}
	j.s.waitFor("durable completion", 10*time.Second, func(string) bool { _, err := os.Stat(j.job.LogPath + ".result"); return err == nil })
	if out, err := j.s.cli("server", "live-handoff", "--import-exe", binPath); err != nil {
		t.Fatal(out, err)
	}
	row, err := j.store.CommandRow(j.job.ID)
	if err != nil || row == nil || row.Status != "running" {
		t.Fatal("uncollected old job deleted", row, err)
	}
	for _, suffix := range []string{"", ".runner", ".result"} {
		if _, err := os.Stat(j.job.LogPath + suffix); err != nil {
			t.Fatal("uncollected evidence deleted", suffix, err)
		}
	}
	faultSQL(t, j.dbPath, `DROP TRIGGER reject_finish`)
	j.s.waitFor("retained result collected", 10*time.Second, func(string) bool {
		row, _ := j.store.CommandRow(j.job.ID)
		return row != nil && row.Status == "exited" && row.ExitCode != nil && *row.ExitCode == 7 && j.notices() == 1
	})
	row, _ = j.store.CommandRow(j.job.ID)
	data, _ := os.ReadFile(row.LogPath)
	if !strings.Contains(string(data), "[stdout] FINAL_OUTPUT\n") {
		t.Fatal("final output lost", string(data))
	}
	writer, err := dispatch.Open(j.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if err := writer.CleanupJobs(time.UnixMilli(*row.FinishedMs).Add(dispatch.JobRetention+time.Millisecond), filepath.Dir(row.LogPath), func(pid int) bool { return syscall.Kill(-pid, 0) == nil }); err != nil {
		t.Fatal(err)
	}
	if row, _ := j.store.CommandRow(j.job.ID); row != nil {
		t.Fatal("terminal retention no longer expires", row)
	}
	t.Log("old created_at and failed collection did not delete evidence; terminal retention still expires")
}

func TestCancelFileFailureKeepsRunningAndRecovers(t *testing.T) {
	t.Parallel()
	j := startFaultJob(t)
	path := j.job.LogPath + ".cancel"
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := j.s.cli("run", "cancel", j.job.ID); err == nil {
		t.Fatal("cancel file failure accepted", out)
	}
	row, err := j.store.CommandRow(j.job.ID)
	if err != nil || row == nil || row.Status != "running" || syscall.Kill(int(*row.RunnerPID), 0) != nil {
		t.Fatal("failed preparation changed job", row, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if out, err := j.s.cli("run", "cancel", j.job.ID); err != nil {
		t.Fatal(out, err)
	}
	j.s.waitFor("cancellation after filesystem recovery", 10*time.Second, func(string) bool {
		row, _ := j.store.CommandRow(j.job.ID)
		return row != nil && row.Status == "cancelled" && row.ExitCode == nil && j.notices() == 1
	})
	t.Log("real .cancel write failure left running; recovery cancelled once")
}

func TestCancellingJobWithoutMonitorTerminatesWithReason(t *testing.T) {
	t.Parallel()
	j := startFaultJob(t)
	data, err := os.ReadFile(j.job.LogPath + ".runner")
	if err != nil {
		t.Fatal(err)
	}
	var record struct{ MonitorPID int }
	if err = json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if err = syscall.Kill(record.MonitorPID, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	j.s.waitFor("helper process reaped", 5*time.Second, func(string) bool { return syscall.Kill(record.MonitorPID, 0) == syscall.ESRCH })
	if out, err := j.s.cli("run", "cancel", j.job.ID); err != nil {
		t.Fatal(out, err)
	}
	j.s.waitFor("group gone without invented exit", 10*time.Second, func(string) bool {
		row, _ := j.store.CommandRow(j.job.ID)
		return row != nil && row.Status == "cancelled" && row.ExitCode == nil && j.notices() == 1
	})
	data, _ = os.ReadFile(j.sink)
	if !strings.Contains(string(data), "completion unavailable") {
		t.Fatal("failure reason missing", string(data))
	}
	t.Log("real helper SIGKILL; group gone; cancelled with nil exit and explicit reason, one notice")
}
