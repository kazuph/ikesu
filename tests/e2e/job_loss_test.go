package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

type lossManifest struct {
	MonitorPID    int
	SupervisorPID int
	PID           int
}

func signalLossProcess(t *testing.T, pid int, signal syscall.Signal, scope string) {
	t.Helper()
	out, err := exec.Command("pgrep", "-u", strconv.Itoa(os.Getuid()), "-f", scope).Output()
	if err != nil || !strings.Contains("\n"+string(out), "\n"+strconv.Itoa(pid)+"\n") {
		t.Fatalf("unverified process %d in %s: %s %v", pid, scope, out, err)
	}
	if err := exec.Command("kill", "-"+strconv.Itoa(int(signal)), strconv.Itoa(pid)).Run(); err != nil {
		t.Fatal(err)
	}
}

func TestLostMonitorPreservesLiveChildAndNotifiesOnce(t *testing.T) {
	j := startFaultJob(t)
	data, err := os.ReadFile(j.job.LogPath + ".runner")
	if err != nil {
		t.Fatal(err)
	}
	var m lossManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	signalLossProcess(t, m.MonitorPID, syscall.SIGKILL, binPath)
	var control api.RunStartedResult
	j.s.rpc(api.MethodRunStart, api.RunStartParams{CallerPane: "p1", Label: "loss-control", Argv: []string{"/bin/echo", "collected"}, Completion: "none"}, &control)
	j.s.waitFor("independent completion while child lives", 10*time.Second, func(string) bool { r, _ := j.store.CommandRow(control.Job); return r != nil && r.Status == "exited" })
	row, err := j.store.CommandRow(j.job.ID)
	if err != nil || row.Status != "running" || j.notices() != 0 {
		t.Fatal(row, err, j.notices())
	}
	state, err := exec.Command("ps", "-o", "stat=", "-p", fmt.Sprint(m.PID)).Output()
	if err != nil || strings.HasPrefix(strings.TrimSpace(string(state)), "Z") {
		t.Fatal("child not live", string(state), err)
	}
	if _, err := j.release.WriteString("go\n"); err != nil {
		t.Fatal(err)
	}
	j.s.waitFor("lost completion recorded and delivered", 10*time.Second, func(string) bool {
		r, _ := j.store.CommandRow(j.job.ID)
		return r != nil && r.Status == "exited" && r.ExitCode == nil && j.notices() == 1
	})
	got, _ := os.ReadFile(j.sink)
	if !strings.Contains(string(got), "exit=unknown") {
		t.Fatal(string(got))
	}
	if out, err := j.s.cli("server", "live-handoff", "--import-exe", binPath); err != nil {
		t.Fatal(out, err)
	}
	if j.notices() != 1 {
		t.Fatal("notice duplicated", j.notices())
	}
}

func TestLostJobRecoveryAcrossHandoffAndDiagnosticFailures(t *testing.T) {
	for _, tc := range []string{"before-handoff", "after-handoff", "stopped-supervisor", "unwritable-diagnostic", "full", "none"} {
		t.Run(tc, func(t *testing.T) {
			completion := "summary"
			if tc == "full" || tc == "none" {
				completion = tc
			}
			j := startFaultJobScriptCompletion(t, func(fifo string) string {
				return fmt.Sprintf("printf 'READY\\n'; read value < %q; printf 'END_MARKER\\n'; exit 7", fifo)
			}, completion)
			data, err := os.ReadFile(j.job.LogPath + ".runner")
			if err != nil {
				t.Fatal(err)
			}
			var m lossManifest
			if err = json.Unmarshal(data, &m); err != nil {
				t.Fatal(err)
			}
			if m.SupervisorPID <= 0 {
				t.Fatal("supervisor identity missing", m)
			}
			if tc == "after-handoff" {
				if out, err := j.s.cli("server", "live-handoff", "--import-exe", binPath); err != nil {
					t.Fatal(out, err)
				}
			}
			if tc == "stopped-supervisor" {
				signalLossProcess(t, m.SupervisorPID, syscall.SIGSTOP, binPath)
				t.Cleanup(func() {
					if syscall.Kill(m.SupervisorPID, 0) == nil {
						signalLossProcess(t, m.SupervisorPID, syscall.SIGCONT, binPath)
					}
				})
			}
			if tc == "unwritable-diagnostic" {
				dir := filepath.Dir(j.job.LogPath)
				if err := os.Chmod(dir, 0500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.Chmod(dir, 0700) })
			}
			signalLossProcess(t, m.MonitorPID, syscall.SIGKILL, binPath)
			if tc == "before-handoff" {
				if out, err := j.s.cli("server", "live-handoff", "--import-exe", binPath); err != nil {
					t.Fatal(out, err)
				}
			}
			if _, err := j.release.WriteString("go\n"); err != nil {
				t.Fatal(err)
			}
			wantNotices := 1
			if completion == "none" {
				wantNotices = 0
			}
			j.s.waitFor("lost result without waiting for diagnostics", 10*time.Second, func(string) bool {
				r, _ := j.store.CommandRow(j.job.ID)
				return r != nil && r.Status == "exited" && r.ExitCode == nil && j.notices() == wantNotices
			})
			got, _ := os.ReadFile(j.sink)
			if completion != "none" && !strings.Contains(string(got), "exit=unknown") {
				t.Fatal(string(got))
			}
			if tc == "full" {
				log, _ := os.ReadFile(j.job.LogPath)
				if !strings.Contains(string(got), "READY") {
					t.Fatal("full log tail missing", string(log), string(got))
				}
			}
			if tc == "unwritable-diagnostic" {
				if !strings.Contains(string(got), "diagnostic save failed") {
					t.Fatal(string(got))
				}
			} else {
				if tc == "stopped-supervisor" {
					if _, err := os.Stat(j.job.LogPath + ".monitor-exit.json"); !os.IsNotExist(err) {
						t.Fatal("stopped supervisor published", err)
					}
					signalLossProcess(t, m.SupervisorPID, syscall.SIGCONT, binPath)
				}
				j.s.waitFor("real monitor Wait signal evidence", 10*time.Second, func(string) bool {
					b, err := os.ReadFile(j.job.LogPath + ".monitor-exit.json")
					return err == nil && strings.Contains(string(b), "killed")
				})
			}
			if out, err := j.s.cli("server", "live-handoff", "--import-exe", binPath); err != nil {
				t.Fatal(out, err)
			}
			if j.notices() != wantNotices {
				t.Fatal("notification repeated", j.notices())
			}
		})
	}
}
