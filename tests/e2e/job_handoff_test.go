package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/dispatch"
)

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
			store, err := dispatch.OpenReadOnly(filepath.Join(s.stateDir, "sessions", s.name, "ikesu.db"))
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

// Stage three intentionally replaces cross-name live upgrades with offline
// migration. Rejection must leave the retiring server's data and PTYs usable.
