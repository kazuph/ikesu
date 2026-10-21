package e2e

import (
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
	"github.com/kazuph/ikesu/internal/envcompat"
)

// teststate.Run clears GTX_*; retain this explicit input before isolation.
var jobHandoffOldBin = envcompat.Get("IKESU_JOB_HANDOFF_OLD_BIN")

func TestJobsAcrossLiveHandoff(t *testing.T) {
	for _, tc := range []struct {
		name     string
		handoffs int
		cancel   bool
	}{
		{"once", 1, false}, {"repeated", 3, false}, {"cancel", 1, true}, {"cancel-term", 1, true}, {"rollback", 1, false}, {"full", 1, false}, {"boundary", 3, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := start(t)
			s.waitFor("shell", 15*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
			s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "job-handoff", Agent: "codex", State: "idle"}, nil)
			fifo := filepath.Join(t.TempDir(), "release")
			if err := syscall.Mkfifo(fifo, 0600); err != nil {
				t.Fatal(err)
			}
			release, err := os.OpenFile(fifo, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer release.Close()
			var started api.RunStartedResult
			script := fmt.Sprintf("trap '' TERM; printf 'READY\\n'; read value < %q; printf 'LAST_OUT\\n'; printf 'LAST_ERR\\n' >&2; exit 7", fifo)
			if tc.name == "cancel-term" {
				script = strings.TrimPrefix(script, "trap '' TERM; ")
			}
			completion := "summary"
			if tc.name == "full" {
				completion = "full"
			}
			s.rpc(api.MethodRunStart, api.RunStartParams{CallerPane: "p1", Label: "handoff", Completion: completion, Argv: []string{"/bin/sh", "-c", script}}, &started)
			store, err := dispatch.OpenReadOnly(filepath.Join(s.stateDir, "sessions", s.name, "gtx.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			initial, err := store.CommandRow(started.Job)
			if err != nil || initial == nil || initial.RunnerPID == nil {
				t.Fatal(initial, err)
			}
			pid := int(*initial.RunnerPID)
			t.Cleanup(func() {
				if syscall.Kill(pid, 0) == nil {
					_ = syscall.Kill(-pid, syscall.SIGKILL)
				}
			})
			s.waitFor("job ready", 10*time.Second, func(string) bool {
				data, _ := os.ReadFile(initial.LogPath)
				return strings.Contains(string(data), "[stdout] READY\n")
			})
			if tc.name == "rollback" {
				if out, err := s.cli("server", "live-handoff", "--import-exe", binPath, "--expected-version", "invalid-version"); err == nil || !strings.Contains(out, "handoff_failed") {
					t.Fatal("rollback not exercised", out, err)
				}
			}
			for i := 0; i < tc.handoffs; i++ {
				if tc.name == "boundary" && i == 0 {
					if _, err := release.WriteString("go\n"); err != nil {
						t.Fatal(err)
					}
				}
				if out, err := s.cli("server", "live-handoff", "--import-exe", binPath); err != nil {
					t.Fatal(out, err)
				}
				row, err := store.CommandRow(started.Job)
				if err != nil || row == nil || row.RunnerPID == nil || *row.RunnerPID != int64(pid) || tc.name != "boundary" && syscall.Kill(pid, 0) != nil {
					t.Fatal("job identity lost", row, err)
				}
			}
			status, notice := "exited", "[ikesu run] exit=7 label=handoff job="+started.Job
			if tc.cancel {
				var cancelled api.RunCancelledResult
				s.rpc(api.MethodRunCancel, api.RunCancelParams{JobID: started.Job}, &cancelled)
				if cancelled.Status != "cancelling" {
					t.Fatal(cancelled)
				}
				if tc.name != "cancel-term" {
					if out, err := s.cli("server", "live-handoff", "--import-exe", binPath); err != nil {
						t.Fatal(out, err)
					}
				}
				status, notice = "cancelled", "[ikesu run] cancelled label=handoff job="+started.Job
			} else if tc.name != "boundary" {
				if _, err = release.WriteString("go\n"); err != nil {
					t.Fatal(err)
				}
			}
			s.waitFor("terminal job and notice", 15*time.Second, func(text string) bool {
				row, _ := store.CommandRow(started.Job)
				return row != nil && row.Status == status && row.FinishedMs != nil && strings.Contains(text, notice)
			})
			row, _ := store.CommandRow(started.Job)
			data, err := os.ReadFile(row.LogPath)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.cancel && (row.ExitCode == nil || *row.ExitCode != 7 || !strings.Contains(string(data), "[stdout] LAST_OUT\n") || !strings.Contains(string(data), "[stderr] LAST_ERR\n")) {
				t.Fatal(row, string(data))
			}
			if tc.cancel && syscall.Kill(pid, 0) == nil {
				t.Fatal("cancelled runner still alive")
			}
			if out, err := s.cli("server", "live-handoff", "--import-exe", binPath); err != nil {
				t.Fatal(out, err)
			}
			s.waitFor("client reattached", 5*time.Second, func(text string) bool { return strings.Contains(text, notice) })
			messages, err := store.History(dispatch.JobsRoom, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, m := range messages {
				if strings.Contains(m.Body, "job="+started.Job+" ") {
					count++
				}
			}
			if count != 1 || strings.Count(s.text(), notice) != 1 {
				t.Fatalf("not exactly once: ledger=%d screen=%d", count, strings.Count(s.text(), notice))
			}
			t.Logf("PID=%d retained across %d handoffs; status=%s; complete log; ledger notices=%d screen notices=1", pid, tc.handoffs, row.Status, count)
		})
	}
}

// The first upgrade cannot reconstruct a legacy parent's wait status or
// missing log pipes. Preserve the accepted legacy running state verbatim.
func TestLegacyJobsAcrossLiveHandoff(t *testing.T) {
	old := jobHandoffOldBin
	if old == "" {
		dir := t.TempDir()
		archive := filepath.Join(dir, "baseline.tar")
		cmd := exec.Command("git", "archive", "--format=tar", "--output", archive, "2e01183")
		cmd.Dir = repoRoot()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("prepare baseline (or set GTX_JOB_HANDOFF_OLD_BIN): %v %s", err, out)
		}
		if out, err := exec.Command("tar", "-xf", archive, "-C", dir).CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
		old = filepath.Join(dir, "gtx-old")
		cmd = exec.Command("go", "build", "-o", old, "./cmd/gtx")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
	}
	// Executable names select independent state roots. Keep the baseline
	// bytes unchanged while starting it under the public gtx name.
	old, err := filepath.Abs(old)
	if err != nil {
		t.Fatal(err)
	}
	canonicalOld := filepath.Join(t.TempDir(), "gtx")
	if err := os.Symlink(old, canonicalOld); err != nil {
		t.Fatal(err)
	}
	for _, successor := range []struct{ name, binary string }{
		{"baseline", canonicalOld}, {"upgrade", binPath},
	} {
		t.Run(successor.name, func(t *testing.T) {
			s := startVersionMode(t, nil, false, binPath, canonicalOld)
			s.waitFor("shell", 15*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
			s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "legacy-job", Agent: "codex", State: "idle"}, nil)
			fifo := filepath.Join(t.TempDir(), "release")
			if err := syscall.Mkfifo(fifo, 0600); err != nil {
				t.Fatal(err)
			}
			release, err := os.OpenFile(fifo, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer release.Close()
			var started api.RunStartedResult
			s.rpc(api.MethodRunStart, api.RunStartParams{CallerPane: "p1", Label: "legacy", Completion: "summary", Argv: []string{"/bin/sh", "-c", fmt.Sprintf("read value < %q; exit 9", fifo)}}, &started)
			store, err := dispatch.OpenReadOnly(filepath.Join(s.stateDir, "sessions", s.name, "gtx.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			before, err := store.CommandRow(started.Job)
			if err != nil || before == nil || before.RunnerPID == nil {
				t.Fatal(before, err)
			}
			pid := int(*before.RunnerPID)
			t.Cleanup(func() {
				if syscall.Kill(pid, 0) == nil {
					_ = syscall.Kill(-pid, syscall.SIGKILL)
				}
			})
			if out, err := s.cli("server", "live-handoff", "--import-exe", successor.binary); err != nil {
				t.Fatal(out, err)
			}
			if syscall.Kill(pid, 0) != nil {
				t.Fatal("legacy job stopped by handoff")
			}
			if _, err := release.WriteString("go\n"); err != nil {
				t.Fatal(err)
			}
			s.waitFor("legacy command exits", 10*time.Second, func(string) bool {
				out, _ := exec.Command("ps", "-o", "stat=", "-p", fmt.Sprint(pid)).Output()
				return strings.TrimSpace(string(out)) == "" || strings.HasPrefix(strings.TrimSpace(string(out)), "Z")
			})
			// Force reconciliation in another importer without changing the row.
			if out, err := s.cli("server", "live-handoff", "--import-exe", successor.binary); err != nil {
				t.Fatal(out, err)
			}
			after, err := store.CommandRow(started.Job)
			if err != nil || after == nil || after.RunnerPID == nil || *after.RunnerPID != *before.RunnerPID || after.Status != "running" || after.ExitCode != nil || after.FinishedMs != nil {
				t.Fatal("legacy state changed", after, err)
			}
			messages, err := store.History(dispatch.JobsRoom, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, message := range messages {
				if strings.Contains(message.Body, "[ikesu run]") {
					t.Fatal("fabricated legacy completion", message)
				}
			}
			t.Logf("legacy PID=%d preserved; after actual exit status=running exit=nil finished=nil, no fabricated notice", pid)
		})
	}
}
