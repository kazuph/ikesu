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
	"github.com/kazuph/ikesu/internal/dispatch"
	"golang.org/x/sys/unix"
	"net"
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
	t.Parallel()
	j := startFaultJob(t)
	data, err := os.ReadFile(j.job.LogPath + ".runner")
	if err != nil {
		t.Fatal(err)
	}
	var m lossManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for _, pid := range []int{m.SupervisorPID, m.MonitorPID, m.PID} {
		pgid, err := syscall.Getpgid(pid)
		if err != nil || pgid != pid {
			t.Fatal("group ownership changed", pid, pgid, err)
		}
		sid, err := unix.Getsid(pid)
		if err != nil {
			t.Fatal(err)
		}
		if pid != m.PID && sid != pid {
			t.Fatal("monitor/reaper is not session leader", pid, sid)
		}
		t.Logf("pid=%d pgid=%d sid=%d", pid, pgid, sid)
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
	t.Parallel()
	for _, tc := range []string{"before-handoff", "after-handoff", "stopped-supervisor", "unwritable-diagnostic", "busy-diagnostic-lock", "full", "none"} {
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

func TestCancelWaitsForStoppedMonitorAndPreservesHandoff(t *testing.T) {
	t.Parallel()
	for _, handoff := range []bool{false, true} {
		t.Run(fmt.Sprint(handoff), func(t *testing.T) {
			j := startFaultJob(t)
			data, err := os.ReadFile(j.job.LogPath + ".runner")
			if err != nil {
				t.Fatal(err)
			}
			var m lossManifest
			if err = json.Unmarshal(data, &m); err != nil {
				t.Fatal(err)
			}
			signalLossProcess(t, m.MonitorPID, syscall.SIGSTOP, binPath)
			t.Cleanup(func() {
				if syscall.Kill(m.MonitorPID, 0) == nil {
					signalLossProcess(t, m.MonitorPID, syscall.SIGCONT, binPath)
				}
			})
			if out, err := j.s.cli("run", "cancel", j.job.ID); err != nil {
				t.Fatal(out, err)
			}
			if handoff {
				if out, err := j.s.cli("server", "live-handoff", "--import-exe", binPath); err != nil {
					t.Fatal(out, err)
				}
			}
			j.s.waitFor("child group stopped before monitor can publish", 10*time.Second, func(string) bool {
				b, err := exec.Command("ps", "-o", "stat=", "-p", fmt.Sprint(m.PID)).Output()
				return err != nil || strings.HasPrefix(strings.TrimSpace(string(b)), "Z")
			})
			r, err := j.store.CommandRow(j.job.ID)
			if err != nil || r.Status != "cancelling" || j.notices() != 0 {
				t.Fatal("live stopped monitor was called gone", r, err, j.notices())
			}
			signalLossProcess(t, m.MonitorPID, syscall.SIGCONT, binPath)
			j.s.waitFor("cancel completed after monitor resumed", 10*time.Second, func(string) bool {
				r, _ := j.store.CommandRow(j.job.ID)
				return r != nil && r.Status == "cancelled" && r.ExitCode == nil && j.notices() == 1
			})
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
	j.s.waitFor("startup recovers result loss once", 10*time.Second, func(string) bool {
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

func TestSSHFailureStillHasActualExitCode(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	j := startFaultJobScript(t, func(fifo string) string {
		return fmt.Sprintf("printf 'READY\\n'; read value < %q; exec /usr/bin/ssh -oBatchMode=yes -oConnectTimeout=5 -p %d 127.0.0.1 true", fifo, port)
	})
	if _, err := j.release.WriteString("go\n"); err != nil {
		t.Fatal(err)
	}
	j.s.waitFor("actual SSH error is collected", 10*time.Second, func(string) bool {
		r, _ := j.store.CommandRow(j.job.ID)
		return r != nil && r.Status == "exited" && r.ExitCode != nil && *r.ExitCode == 255 && j.notices() == 1
	})
}

func TestMonitorNormalExitEvidenceIsSeparateFromCommandExit(t *testing.T) {
	t.Parallel()
	j := startFaultJob(t)
	if _, err := j.release.WriteString("go\n"); err != nil {
		t.Fatal(err)
	}
	j.s.waitFor("command and monitor finish with different actual codes", 10*time.Second, func(string) bool {
		row, _ := j.store.CommandRow(j.job.ID)
		if row == nil || row.Status != "exited" || row.ExitCode == nil || *row.ExitCode != 7 {
			return false
		}
		data, err := os.ReadFile(j.job.LogPath + ".monitor-exit.json")
		if err != nil {
			return false
		}
		var evidence struct {
			ExitCode *int
			Signal   *string
		}
		if err := json.Unmarshal(data, &evidence); err != nil {
			t.Fatal(err)
		}
		return evidence.ExitCode != nil && *evidence.ExitCode == 0 && evidence.Signal == nil && j.notices() == 1
	})
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
	j.s.waitFor("normal result collected while reaper is paused", 10*time.Second, func(string) bool {
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
