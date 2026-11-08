package server

import (
	"github.com/kazuph/ikesu/internal/teststate"

	"os/exec"

	"syscall"
	"testing"

	"github.com/kazuph/ikesu/internal/dispatch"
	"github.com/kazuph/ikesu/internal/render"
)

func openStore(t *testing.T) *dispatch.Store {
	t.Helper()
	st, err := dispatch.OpenReadOnly(dispatch.DBPath("test"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// SPEC G2 workspace job dot: a running job with a live runner shows one
// ● on the caller pane's workspace row; killing the runner out-of-band
// drops the dot at the next snapshot (runner liveness is checked at
// snapshot time, not during render), and a finished job keeps its dot
// for one minute.
func TestWorkspaceJobDot(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "jobdot")
	s.app.SetJobSource(s.jobSnapshot)

	st, err := dispatch.Open(dispatch.DBPath(s.session))
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
