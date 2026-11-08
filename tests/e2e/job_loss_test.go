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

	"github.com/kazuph/ikesu/internal/dispatch"
	"golang.org/x/sys/unix"
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

func TestLostJobRecoveryAcrossHandoffAndDiagnosticFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []string{"before-handoff", "after-handoff", "stopped-supervisor", "unwritable-diagnostic", "busy-diagnostic-lock", "full", "none"} {
		t.Run(tc, func(t *testing.T) {
			t.Parallel()
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
			var heldLog *os.File
			if tc == "busy-diagnostic-lock" {
				heldLog, err = os.Open(j.job.LogPath)
				if err != nil {
					t.Fatal(err)
				}
				if err = unix.Flock(int(heldLog.Fd()), unix.LOCK_EX); err != nil {
					heldLog.Close()
					t.Fatal(err)
				}
				t.Cleanup(func() { heldLog.Close() })
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
			j.s.waitFor("lost result without waiting for diagnostics", noticeWait, func(string) bool {
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
				if tc == "busy-diagnostic-lock" {
					if !strings.Contains(string(got), "diagnostic save failed") {
						t.Fatal("busy diagnostic did not report save failure", string(got))
					}
					if err = unix.Flock(int(heldLog.Fd()), unix.LOCK_UN); err != nil {
						t.Fatal(err)
					}
				}
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

func TestLostJobAfterAllOwnerProcessesDisappearAndRestart(t *testing.T) {
	t.Parallel()
	j := startFaultJob(t)
	data, err := os.ReadFile(j.job.LogPath + ".runner")
	if err != nil {
		t.Fatal(err)
	}
	var m lossManifest
	if err = json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	parent, err := exec.Command("ps", "-o", "ppid=", "-p", fmt.Sprint(m.SupervisorPID)).Output()
	if err != nil {
		t.Fatal(err)
	}
	serverPID, err := strconv.Atoi(strings.TrimSpace(string(parent)))
	if err != nil {
		t.Fatal(err)
	}
	j.s.waitFor("session saved before process loss", 5*time.Second, func(string) bool {
		_, err := os.Stat(filepath.Join(j.s.stateDir, j.s.name, "session.json"))
		return err == nil
	})
	signalLossProcess(t, serverPID, syscall.SIGKILL, binPath)
	signalLossProcess(t, m.SupervisorPID, syscall.SIGKILL, binPath)
	signalLossProcess(t, m.MonitorPID, syscall.SIGKILL, binPath)
	signalLossProcess(t, m.PID, syscall.SIGKILL, j.release.Name())
	j.s.waitFor("old server gone", 5*time.Second, func(string) bool {
		b, err := exec.Command("ps", "-o", "stat=", "-p", fmt.Sprint(serverPID)).Output()
		return err != nil || strings.HasPrefix(strings.TrimSpace(string(b)), "Z")
	})
	successor := exec.Command(binPath, "--session", j.s.name, "server")
	successor.Env = j.s.cmd.Env
	successor.Stderr = os.Stderr
	if err := successor.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		j.s.stopServer()
		if err := successor.Wait(); err != nil {
			t.Error("successor exit", err)
		}
	})
	notices := func() int {
		messages, err := j.store.History(dispatch.JobsRoom, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, msg := range messages {
			if strings.Contains(msg.Body, "job="+j.job.ID+" ") {
				n++
			}
		}
		return n
	}
	j.s.waitFor("startup recovers result loss once", noticeWait, func(string) bool {
		r, _ := j.store.CommandRow(j.job.ID)
		return r != nil && r.Status == "exited" && r.ExitCode == nil && notices() == 1
	})
	if out, err := j.s.cli("server", "live-handoff", "--import-exe", binPath); err != nil {
		t.Fatal(out, err)
	}
	if notices() != 1 {
		t.Fatal("recovery duplicated notice", notices())
	}
}

func TestLateMonitorEvidenceDoesNotAttachToReusedLogPath(t *testing.T) {
	t.Parallel()
	j := startFaultJob(t)
	data, err := os.ReadFile(j.job.LogPath + ".runner")
	if err != nil {
		t.Fatal(err)
	}
	var m lossManifest
	if err = json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	signalLossProcess(t, m.SupervisorPID, syscall.SIGSTOP, binPath)
	resumed := false
	t.Cleanup(func() {
		if !resumed && syscall.Kill(m.SupervisorPID, 0) == nil {
			signalLossProcess(t, m.SupervisorPID, syscall.SIGCONT, binPath)
		}
	})
	if _, err = j.release.WriteString("go\n"); err != nil {
		t.Fatal(err)
	}
	j.s.waitFor("normal result collected while reaper is paused", noticeWait, func(string) bool {
		r, _ := j.store.CommandRow(j.job.ID)
		return r != nil && r.Status == "exited" && r.ExitCode != nil && *r.ExitCode == 7 && j.notices() == 1
	})
	for _, suffix := range []string{"", ".runner", ".result", ".cancel", ".monitor-stderr.log", ".completion-unavailable.json"} {
		if err := os.Remove(j.job.LogPath + suffix); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(j.job.LogPath, []byte("new owner"), 0600); err != nil {
		t.Fatal(err)
	}
	signalLossProcess(t, m.SupervisorPID, syscall.SIGCONT, binPath)
	resumed = true
	j.s.waitFor("old reaper completed", 10*time.Second, func(string) bool {
		b, e := exec.Command("ps", "-o", "stat=", "-p", fmt.Sprint(m.SupervisorPID)).Output()
		return e != nil || strings.HasPrefix(strings.TrimSpace(string(b)), "Z")
	})
	if _, err := os.Stat(j.job.LogPath + ".monitor-exit.json"); !os.IsNotExist(err) {
		t.Fatal("retired job attached evidence to new owner", err)
	}
	data, err = os.ReadFile(j.job.LogPath)
	if err != nil || string(data) != "new owner" {
		t.Fatal(string(data), err)
	}
	if j.notices() != 1 {
		t.Fatal("late evidence sent another notice", j.notices())
	}
}
