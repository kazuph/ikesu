package server

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/dispatch"
	"github.com/kazuph/goterrace/internal/render"
)

func openStore(t *testing.T) *dispatch.Store {
	t.Helper()
	st, err := dispatch.OpenReadOnly(dispatch.DBPath())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func jobStatus(t *testing.T, st *dispatch.Store, id string) string {
	t.Helper()
	j, err := st.CommandRow(id)
	if err != nil || j == nil {
		t.Fatalf("job %s: %v", id, err)
	}
	return j.Status
}

// SPEC G9: cancel must verify the runner is its own process-group leader,
// move to 'cancelling', TERM the group, escalate to KILL after 2 s, and
// only report 'cancelled' once the whole group is gone. A runner whose
// leader ignores TERM must still be fully reaped.
func TestRunCancelEscalatesAndConfirmsGroupGone(t *testing.T) {
	dir, done := startTestServer(t)
	dialClient(t, dir) // keep the server alive while jobs run

	var started api.RunStartedResult
	apiOK(t, dir, api.MethodRunStart, api.RunStartParams{
		Label:      "cancel-me",
		Argv:       []string{"/bin/sh", "-c", `trap "" TERM; echo ready; while :; do sleep 60; done`},
		Completion: "none",
	}, &started)

	st := openStore(t)
	job, err := st.CommandRow(started.Job)
	if err != nil || job == nil || job.RunnerPID == nil {
		t.Fatalf("job row after start: %+v err=%v", job, err)
	}
	pgid := int(*job.RunnerPID)

	// Wait for the runner to install its TERM handler — without the
	// marker, a fast cancel races the trap setup and the whole group dies
	// to TERM, skipping the escalation path this test exists to cover.
	ready := time.Now().Add(10 * time.Second)
	for {
		data, _ := os.ReadFile(job.LogPath)
		if strings.Contains(string(data), "ready") {
			break
		}
		if time.Now().After(ready) {
			t.Fatal("runner never printed ready")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if g, err := syscall.Getpgid(pgid); err != nil || g != pgid {
		t.Fatalf("runner %d must lead group %d: getpgid=%d err=%v", pgid, pgid, g, err)
	}

	var cancel api.RunCancelledResult
	apiOK(t, dir, api.MethodRunCancel, api.RunCancelParams{JobID: started.Job}, &cancel)
	if cancel.Status != "cancelling" {
		t.Fatalf("cancel must not claim early success: %+v", cancel)
	}

	// The runner ignores TERM so the group must survive past the 2s grace
	// window and be reaped by the KILL escalation — never left 'cancelling'
	// once it is actually dead.
	deadline := time.Now().Add(15 * time.Second)
	start := time.Now()
	sawCancelling := false
	for time.Now().Before(deadline) {
		st2, err := dispatch.OpenReadOnly(dispatch.DBPath())
		if err != nil {
			t.Fatal(err)
		}
		cur := jobStatus(t, st2, started.Job)
		st2.Close()
		if cur == "cancelling" {
			sawCancelling = true
		}
		if cur == "cancelled" {
			t.Logf("cancelled after %v", time.Since(start))
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !sawCancelling {
		t.Fatal("row never entered 'cancelling'")
	}
	if e := time.Since(start); e < 1500*time.Millisecond {
		t.Fatalf("cancelled in %v — group must outlive the 2s TERM grace before KILL", e)
	}
	final, _ := st.CommandRow(started.Job)
	if final.Status != "cancelled" {
		t.Fatalf("final status %q, want cancelled", final.Status)
	}
	if processGroupAlive(pgid) {
		t.Fatalf("process group %d survived cancel", pgid)
	}

	apiOK(t, dir, "server.stop", map[string]any{}, nil)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("server did not exit")
	}
}

// SPEC G2 workspace job dot: a running job with a live runner shows one
// ● on the caller pane's workspace row; killing the runner out-of-band
// drops the dot at the next snapshot (runner liveness is checked at
// snapshot time, not during render), and a finished job keeps its dot
// for one minute.
func TestWorkspaceJobDot(t *testing.T) {
	s := newTestServer(t, "jobdot")
	s.app.SetJobSource(s.jobSnapshot)

	st, err := dispatch.Open(dispatch.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// A real detached runner in its own process group.
	cmd := exec.Command("/bin/sh", "-c", "sleep 30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); cmd.Wait() }()

	if err := st.InsertCommand("j0001", "dot-job", "sleep 30", "/tmp", "p1", "", "none", "/tmp/j.log"); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkCommandRunning("j0001", int64(cmd.Process.Pid)); err != nil {
		t.Fatal(err)
	}

	s.app.TestRefreshJobDots()
	if n := len(s.app.JobDots()[0]); n != 1 {
		t.Fatalf("running job dots = %d, want 1", n)
	}

	// Kill the runner group out-of-band; the row still says 'running'
	// but the dot must disappear once the snapshot notices a dead runner.
	syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	cmd.Wait()
	s.app.TestRefreshJobDots()
	view := render.SidebarViewForInput(render.Input{State: s.app.State(), Jobs: s.app.Jobs(), JobsPanel: true})
	if len(s.app.Jobs()) != 1 || view.Agents.Total != 1 {
		t.Fatal("dead runner was filtered out of jobs history")
	}
	if n := len(s.app.JobDots()[0]); n != 0 {
		t.Fatalf("dead-runner dots = %d, want 0", n)
	}

	// A job that finished within the last minute keeps its dot.
	zero := int64(0)
	if _, err := st.MarkCommandFinished("j0001", &zero); err != nil {
		t.Fatal(err)
	}
	s.app.TestRefreshJobDots()
	if n := len(s.app.JobDots()[0]); n != 1 {
		t.Fatalf("just-finished dots = %d, want 1", n)
	}
}
