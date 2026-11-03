//go:build darwin

package e2e

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/tui"
)

// Signal only a process whose PID and command match this test's executable.
func signalUpgradeTestProcess(t *testing.T, pid int, executable, signal string) {
	t.Helper()
	out, err := exec.Command("pgrep", "-u", strconv.Itoa(os.Getuid()), "-f", executable).Output()
	if err != nil {
		t.Fatalf("find test process %d: %v", pid, err)
	}
	found := false
	for _, field := range strings.Fields(string(out)) {
		if field == strconv.Itoa(pid) {
			found = true
		}
	}
	if !found {
		t.Fatalf("process %d is not using test executable %s", pid, executable)
	}
	if out, err := exec.Command("kill", "-"+signal, strconv.Itoa(pid)).CombinedOutput(); err != nil {
		t.Fatalf("signal test process: %v %s", err, out)
	}
}

// Real exec reaches the OS argument limit; no exec function is replaced.
func TestClientRestoresTerminalAndInputAfterExecFailure(t *testing.T) {
	_, oldBinary, newBinary, oldVersion, newVersion := buildClientUpgradeVersions(t)
	resolvedOld, err := filepath.EvalSymlinks(oldBinary)
	if err != nil {
		t.Fatal(err)
	}
	resolvedNew, err := filepath.EvalSymlinks(newBinary)
	if err != nil {
		t.Fatal(err)
	}
	state, err := json.Marshal(map[string]any{
		"old_executable": resolvedOld, "old_version": oldVersion, "session": "e2e", "active": "local",
		"attempt": map[string]any{"session": "e2e", "generation": rand.Text(), "executable": resolvedNew, "version": newVersion},
	})
	if err != nil {
		t.Fatal(err)
	}
	prepare := func(cmd *exec.Cmd) {
		t.Helper()
		out, err := exec.Command("sysctl", "-n", "kern.argmax").Output()
		if err != nil {
			t.Fatal(err)
		}
		maximum, err := strconv.Atoi(strings.TrimSpace(string(out)))
		if err != nil {
			t.Fatal(err)
		}
		probe := func(n int, upgrade bool) error {
			probeCmd := exec.Command(oldBinary, "--session", "e2e", "--version")
			probeCmd.Env = append(append([]string(nil), cmd.Env...), "CLIENT_UPGRADE_PADDING="+strings.Repeat("x", n))
			if upgrade {
				probeCmd.Env = append(probeCmd.Env, "IKESU_CLIENT_UPGRADE="+string(state))
			}
			return probeCmd.Run()
		}
		low, high := 0, maximum
		for low < high {
			middle := low + (high-low+1)/2
			err := probe(middle, false)
			if err == nil {
				low = middle
			} else if errors.Is(err, syscall.E2BIG) {
				high = middle - 1
			} else {
				t.Fatalf("calibrate argument limit: %v", err)
			}
		}
		// The probe has an extra --version argument; leave room for the
		// resolved executable path while keeping less than one state payload.
		padding := low - (len(resolvedOld) - len(oldBinary)) - len("--version")
		if padding < 0 || probe(padding, false) != nil || !errors.Is(probe(padding, true), syscall.E2BIG) {
			t.Fatal("could not create a real exec limit boundary")
		}
		cmd.Env = append(cmd.Env, "CLIENT_UPGRADE_PADDING="+strings.Repeat("x", padding))
	}
	s := startVersionModePrepared(t, nil, false, oldBinary, oldBinary, prepare)
	s.waitFor("old client", 15*time.Second, func(text string) bool { return strings.Contains(text, "ikesu v"+oldVersion) })
	var output bytes.Buffer
	s.observeOutput = func(data []byte) { _, _ = output.Write(data) }
	if out, err := s.cli("server", "live-handoff", "--import-exe", newBinary); err != nil {
		t.Fatalf("handoff: %v %s", err, out)
	}
	s.send("printf 'BUFFERED_BEFORE_EXEC_FAILURE\\n'\r")
	s.waitFor("exec failure reason", 15*time.Second, func(text string) bool { return strings.Contains(text, "upgrade: argument") })
	s.waitFor("buffered input after failed exec", 10*time.Second, func(text string) bool { return strings.Contains(text, "BUFFERED_BEFORE_EXEC_FAILURE") })
	if !bytes.Contains(output.Bytes(), []byte(tui.LeaveSeq)) || !bytes.Contains(output.Bytes(), []byte(tui.EnterSeq)) {
		t.Fatal("terminal was not restored and entered again after exec failure")
	}
	s.send("printf 'INPUT_AFTER_EXEC_FAILURE\\n'\r")
	s.waitFor("input after failed exec", 10*time.Second, func(text string) bool { return strings.Contains(text, "INPUT_AFTER_EXEC_FAILURE") })
	var read api.PaneReadResultEnvelope
	s.rpc(api.MethodPaneRead, api.PaneReadParams{PaneID: "p1", StripANSI: true, Source: "recent", Format: "text"}, &read)
	if !strings.Contains(read.Read.Text, "BUFFERED_BEFORE_EXEC_FAILURE") || !strings.Contains(read.Read.Text, "INPUT_AFTER_EXEC_FAILURE") {
		t.Fatalf("input did not reach the original pane: %s", read.Read.Text)
	}
}

// This waits for the terminal drain boundary before replacing the verified file.
func TestClientRejectsExecutableReplacedAfterVersionCheck(t *testing.T) {
	dir, oldBinary, newBinary, oldVersion, _ := buildClientUpgradeVersions(t)
	s := startVersionMode(t, nil, false, oldBinary, oldBinary)
	s.waitFor("old client", 15*time.Second, func(text string) bool { return strings.Contains(text, "ikesu v"+oldVersion) })
	if out, err := s.cli("server", "live-handoff", "--import-exe", newBinary); err != nil {
		t.Fatalf("handoff: %v %s", err, out)
	}
	var output bytes.Buffer
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	for !bytes.Contains(output.Bytes(), []byte(tui.MouseOffSeq)) {
		select {
		case data, ok := <-s.buf.ch:
			if !ok {
				t.Fatal("client closed before terminal drain")
			}
			_, _ = output.Write(data)
			_, _ = s.emu.Write(data)
		case <-deadline.C:
			t.Fatal("client did not reach terminal drain")
		}
	}
	pid := s.cmd.Process.Pid
	signalUpgradeTestProcess(t, pid, oldBinary, "STOP")
	stopped := true
	defer func() {
		if stopped {
			signalUpgradeTestProcess(t, pid, oldBinary, "CONT")
		}
	}()
	// This input arrives after the reader stopped and must survive rejection.
	s.send("printf 'BUFFERED_DURING_REPLACEMENT\\n'\r")
	if err := os.Rename(newBinary, filepath.Join(dir, "running-server")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(oldBinary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newBinary, data, 0700); err != nil {
		t.Fatal(err)
	}
	signalUpgradeTestProcess(t, pid, oldBinary, "CONT")
	stopped = false
	s.observeOutput = func(data []byte) { _, _ = output.Write(data) }
	s.waitFor("replacement rejection", 15*time.Second, func(text string) bool { return strings.Contains(text, "upgrade:") })
	if text := s.text(); !strings.Contains(text, "file changed") {
		t.Fatalf("replacement reason is missing:\n%s", text)
	}
	s.waitFor("buffered input after replacement", 10*time.Second, func(text string) bool { return strings.Contains(text, "BUFFERED_DURING_REPLACEMENT") })
	var read api.PaneReadResultEnvelope
	s.rpc(api.MethodPaneRead, api.PaneReadParams{PaneID: "p1", StripANSI: true, Source: "recent", Format: "text"}, &read)
	if !strings.Contains(read.Read.Text, "BUFFERED_DURING_REPLACEMENT") {
		t.Fatalf("buffered input did not reach original pane: %s", read.Read.Text)
	}
	if !bytes.Contains(output.Bytes(), []byte(tui.LeaveSeq)) || !bytes.Contains(output.Bytes(), []byte(tui.EnterSeq)) {
		t.Fatal("terminal did not leave and re-enter after rejection")
	}
}

// The timeout case must wait for the real client's ten-second version deadline.
func TestClientKeepsRunningOnRejectedUpgradeExecutables(t *testing.T) {
	for _, fault := range []string{"not-executable", "broken-link", "version-timeout"} {
		t.Run(fault, func(t *testing.T) {
			dir, oldBinary, newBinary, oldVersion, _ := buildClientUpgradeVersions(t)
			resolvedBinary, err := filepath.EvalSymlinks(newBinary)
			if err != nil {
				t.Fatal(err)
			}
			s := startVersionMode(t, nil, false, oldBinary, oldBinary)
			s.waitFor("old client", 15*time.Second, func(text string) bool { return strings.Contains(text, "ikesu v"+oldVersion) })
			var output bytes.Buffer
			s.observeOutput = func(data []byte) { _, _ = output.Write(data) }
			pid := s.cmd.Process.Pid
			signalUpgradeTestProcess(t, pid, oldBinary, "STOP")
			stopped := true
			defer func() {
				if stopped {
					signalUpgradeTestProcess(t, pid, oldBinary, "CONT")
				}
			}()
			if out, err := s.cli("server", "live-handoff", "--import-exe", newBinary); err != nil {
				t.Fatalf("handoff: %v %s", err, out)
			}
			switch fault {
			case "not-executable":
				if err := os.Chmod(newBinary, 0600); err != nil {
					t.Fatal(err)
				}
			case "broken-link", "version-timeout":
				if err := os.Rename(newBinary, filepath.Join(dir, "running-server")); err != nil {
					t.Fatal(err)
				}
				if fault == "broken-link" {
					if err := os.Symlink(filepath.Join(dir, "missing"), newBinary); err != nil {
						t.Fatal(err)
					}
				} else {
					data, err := os.ReadFile("/usr/bin/yes")
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(newBinary, data, 0700); err != nil {
						t.Fatal(err)
					}
					if out, err := exec.Command("codesign", "--force", "--sign", "-", newBinary).CombinedOutput(); err != nil {
						t.Fatalf("sign system executable copy: %v %s", err, out)
					}
				}
			}
			signalUpgradeTestProcess(t, pid, oldBinary, "CONT")
			stopped = false
			if fault == "version-timeout" {
				// Wait for the real --version child and stop only that verified PID.
				deadline := time.NewTimer(5 * time.Second)
				ticker := time.NewTicker(10 * time.Millisecond)
				defer deadline.Stop()
				defer ticker.Stop()
				child := 0
				for child == 0 {
					select {
					case <-deadline.C:
						s.pump(time.Now().Add(100 * time.Millisecond))
						t.Fatalf("version process did not start; screen:\n%s", s.text())
					case <-ticker.C:
						out, _ := exec.Command("pgrep", "-u", strconv.Itoa(os.Getuid()), "-f", resolvedBinary).Output()
						for _, field := range strings.Fields(string(out)) {
							candidate, err := strconv.Atoi(field)
							if err != nil {
								continue
							}
							info, _ := exec.Command("ps", "-ww", "-p", field, "-o", "ppid=", "-o", "command=").Output()
							fields := strings.Fields(string(info))
							if len(fields) >= 3 && fields[0] == strconv.Itoa(pid) && fields[1] == resolvedBinary && fields[2] == "--version" {
								child = candidate
								break
							}
						}
					}
				}
				signalUpgradeTestProcess(t, child, resolvedBinary, "STOP")
				s.send("printf 'INPUT_DURING_VERSION_CHECK\\n'\r")
				s.waitFor("input during version check", 5*time.Second, func(text string) bool { return strings.Contains(text, "INPUT_DURING_VERSION_CHECK") })
			}
			s.waitFor("upgrade failure reason", 15*time.Second, func(text string) bool { return strings.Contains(text, "upgrade:") })
			if bytes.Contains(output.Bytes(), []byte(tui.LeaveSeq)) {
				t.Fatal("rejected candidate closed the old terminal")
			}
			s.send("printf 'INPUT_AFTER_UPGRADE_REJECTION\\n'\r")
			s.waitFor("input after rejection", 5*time.Second, func(text string) bool { return strings.Contains(text, "INPUT_AFTER_UPGRADE_REJECTION") })
		})
	}
}
