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

	"github.com/creack/pty"
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

// Real exec reaches the OS argument limit; binary builds and boundary probes can take more than five seconds.
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

// Building the instrumented old client and performing a real handoff can exceed five seconds.
func TestClientRejectsExecutableReplacedAfterVersionCheck(t *testing.T) {
	dir, oldBinary, newBinary, oldVersion, _ := buildStoppedClientUpgradeVersions(t, false)
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
	waitForUpgradeTestStop(t, pid, deadline.C)
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

func waitForUpgradeTestStop(t *testing.T, pid int, deadline <-chan time.Time) {
	t.Helper()
	for {
		state, err := exec.Command("ps", "-o", "state=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			t.Fatalf("read client state: %v", err)
		}
		if strings.HasPrefix(strings.TrimSpace(string(state)), "T") {
			break
		}
		select {
		case <-deadline:
			t.Fatal("client did not stop at final ping")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

// Real client builds, a live handoff, and rollback can exceed five seconds under concurrent load.
func TestClientPreservesInputAcrossConfigFailureRollback(t *testing.T) {
	t.Parallel()
	_, oldBinary, newBinary, oldVersion, _ := buildStoppedClientUpgradeVersions(t, true)
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte("onboarding=false\n[session]\nresume_agents_on_restore=false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := startVersionMode(t, map[string]string{"IKESU_CONFIG": cfg}, false, oldBinary, oldBinary)
	s.waitFor("old client", 10*time.Second, func(text string) bool { return strings.Contains(text, "ikesu v"+oldVersion) })
	if out, err := s.cli("server", "live-handoff", "--import-exe", newBinary); err != nil {
		t.Fatalf("handoff: %v %s", err, out)
	}
	var output bytes.Buffer
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for !bytes.Contains(output.Bytes(), []byte(tui.MouseOffSeq)) {
		select {
		case data, ok := <-s.buf.ch:
			if !ok {
				t.Fatal("client exited before upgrade")
			}
			output.Write(data)
			_, _ = s.emu.Write(data)
		case <-timer.C:
			t.Fatal("client did not reach upgrade drain")
		}
	}
	pid := s.cmd.Process.Pid
	waitForUpgradeTestStop(t, pid, timer.C)
	stopped := true
	defer func() {
		if stopped {
			signalUpgradeTestProcess(t, pid, oldBinary, "CONT")
		}
	}()
	s.send("printf 'BUFFERED_BEFORE_CONFIG_ROLLBACK\\n'\r")
	if err := os.WriteFile(cfg, []byte("invalid = [\n"), 0600); err != nil {
		t.Fatal(err)
	}
	signalUpgradeTestProcess(t, pid, oldBinary, "CONT")
	stopped = false
	s.waitFor("configuration failure after actual upgrade", 10*time.Second, func(text string) bool { return strings.Contains(text, "upgrade: client config") })
	s.waitFor("buffered input after configuration rollback", 10*time.Second, func(text string) bool { return strings.Contains(text, "BUFFERED_BEFORE_CONFIG_ROLLBACK") })
	var read api.PaneReadResultEnvelope
	s.rpc(api.MethodPaneRead, api.PaneReadParams{PaneID: "p1", StripANSI: true, Source: "recent", Format: "text"}, &read)
	if !strings.Contains(read.Read.Text, "BUFFERED_BEFORE_CONFIG_ROLLBACK") {
		t.Fatalf("input missing from original pane: %s", read.Read.Text)
	}
}

// Real client builds and a live handoff can exceed five seconds under concurrent load.
func TestClientPreservesInputAcrossSuccessfulUpgrade(t *testing.T) {
	t.Parallel()
	_, oldBinary, newBinary, oldVersion, newVersion := buildStoppedClientUpgradeVersions(t, true)
	s := startVersionMode(t, nil, false, oldBinary, oldBinary)
	s.waitFor("old client", 10*time.Second, func(text string) bool { return strings.Contains(text, "ikesu v"+oldVersion) })
	if out, err := s.cli("server", "live-handoff", "--import-exe", newBinary); err != nil {
		t.Fatalf("handoff: %v %s", err, out)
	}
	var output bytes.Buffer
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for !bytes.Contains(output.Bytes(), []byte(tui.MouseOffSeq)) {
		select {
		case data, ok := <-s.buf.ch:
			if !ok {
				t.Fatal("client exited before upgrade")
			}
			output.Write(data)
			_, _ = s.emu.Write(data)
		case <-timer.C:
			t.Fatal("client did not reach upgrade drain")
		}
	}
	pid := s.cmd.Process.Pid
	waitForUpgradeTestStop(t, pid, timer.C)
	stopped := true
	defer func() {
		if stopped {
			signalUpgradeTestProcess(t, pid, oldBinary, "CONT")
		}
	}()
	s.send("printf 'BUFFERED_BEFORE_SUCCESSFUL_UPGRADE\\n'\r")
	signalUpgradeTestProcess(t, pid, oldBinary, "CONT")
	stopped = false
	s.waitFor("new client", 10*time.Second, func(text string) bool { return strings.Contains(text, "ikesu v"+newVersion) })
	s.waitFor("buffered input after successful upgrade", 10*time.Second, func(text string) bool { return strings.Contains(text, "BUFFERED_BEFORE_SUCCESSFUL_UPGRADE") })
	var read api.PaneReadResultEnvelope
	s.rpc(api.MethodPaneRead, api.PaneReadParams{PaneID: "p1", StripANSI: true, Source: "recent", Format: "text"}, &read)
	if !strings.Contains(read.Read.Text, "BUFFERED_BEFORE_SUCCESSFUL_UPGRADE") {
		t.Fatalf("input missing from original pane: %s", read.Read.Text)
	}
}

// A real live handoff and a paste split across two processes can exceed five seconds.
func TestClientPreservesPartialPasteAcrossUpgrade(t *testing.T) {
	t.Parallel()
	_, oldBinary, newBinary, oldVersion, newVersion := buildClientUpgradeVersionsWithShortAttachTimeout(t)
	s := startVersionMode(t, nil, false, oldBinary, oldBinary)
	s.waitFor("old client", 10*time.Second, func(text string) bool { return strings.Contains(text, "ikesu v"+oldVersion) })
	s.send("\x1b[200~printf 'PASTE_%s\\n' '")
	if out, err := s.cli("server", "live-handoff", "--import-exe", newBinary); err != nil {
		t.Fatalf("handoff: %v %s", err, out)
	}
	s.waitFor("new client", 10*time.Second, func(text string) bool { return strings.Contains(text, "ikesu v"+newVersion) })
	s.send("ONCE'\x1b[201~\r")
	s.waitFor("completed paste", 10*time.Second, func(text string) bool { return strings.Contains(text, "PASTE_ONCE") })
	var read api.PaneReadResultEnvelope
	s.rpc(api.MethodPaneRead, api.PaneReadParams{PaneID: "p1", StripANSI: true, Source: "recent", Format: "text"}, &read)
	if count := strings.Count(read.Read.Text, "PASTE_ONCE"); count != 1 {
		t.Fatalf("paste delivered %d times: %s", count, read.Read.Text)
	}
}

// Real client builds, a handoff, and a rollback can exceed five seconds.
func TestClientPreservesPartialPasteAcrossRollback(t *testing.T) {
	t.Parallel()
	_, oldBinary, newBinary, oldVersion, _ := buildStoppedClientUpgradeVersions(t, true)
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte("onboarding=false\n[session]\nresume_agents_on_restore=false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := startVersionMode(t, map[string]string{"IKESU_CONFIG": cfg}, false, oldBinary, oldBinary)
	s.waitFor("old client", 10*time.Second, func(text string) bool { return strings.Contains(text, "ikesu v"+oldVersion) })
	s.send("\x1b[200~printf 'ROLLBACK_PASTE_%s\\n' '")
	if out, err := s.cli("server", "live-handoff", "--import-exe", newBinary); err != nil {
		t.Fatalf("handoff: %v %s", err, out)
	}
	var output bytes.Buffer
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	for !bytes.Contains(output.Bytes(), []byte(tui.MouseOffSeq)) {
		select {
		case data, ok := <-s.buf.ch:
			if !ok {
				t.Fatal("client exited before upgrade")
			}
			output.Write(data)
			_, _ = s.emu.Write(data)
		case <-timer.C:
			t.Fatal("client did not reach upgrade drain")
		}
	}
	pid := s.cmd.Process.Pid
	waitForUpgradeTestStop(t, pid, timer.C)
	stopped := true
	defer func() {
		if stopped {
			signalUpgradeTestProcess(t, pid, oldBinary, "CONT")
		}
	}()
	if err := os.WriteFile(cfg, []byte("invalid = [\n"), 0600); err != nil {
		t.Fatal(err)
	}
	signalUpgradeTestProcess(t, pid, oldBinary, "CONT")
	stopped = false
	s.waitFor("configuration failure after actual upgrade", 10*time.Second, func(text string) bool { return strings.Contains(text, "upgrade: client config") })
	s.send("ONCE'\x1b[201~\r")
	s.waitFor("completed paste after rollback", 10*time.Second, func(text string) bool { return strings.Contains(text, "ROLLBACK_PASTE_ONCE") })
	var read api.PaneReadResultEnvelope
	s.rpc(api.MethodPaneRead, api.PaneReadParams{PaneID: "p1", StripANSI: true, Source: "recent", Format: "text"}, &read)
	if count := strings.Count(read.Read.Text, "ROLLBACK_PASTE_ONCE"); count != 1 {
		t.Fatalf("paste delivered %d times: %s", count, read.Read.Text)
	}
}

// Check each received PTY update as soon as it arrives.
func waitForUpgradeScreen(s *session, what string, timeout time.Duration, pred func(string) bool) {
	s.t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		if pred(s.text()) {
			return
		}
		select {
		case data, ok := <-s.buf.ch:
			if !ok {
				s.t.Fatalf("client exited while waiting for %s; screen:\n%s", what, s.text())
			}
			if s.observeOutput != nil {
				s.observeOutput(data)
			}
			_, _ = s.emu.Write(data)
		case <-timer.C:
			s.t.Fatalf("timed out waiting for %s; screen:\n%s", what, s.text())
		}
	}
}

// The timeout case uses a test build with a shorter version deadline.
func TestClientKeepsRunningOnRejectedUpgradeExecutables(t *testing.T) {
	for _, fault := range []string{"not-executable", "broken-link", "version-timeout"} {
		t.Run(fault, func(t *testing.T) {
			t.Parallel()
			dir, oldBinary, newBinary, oldVersion, _ := buildClientUpgradeVersionsWithShortVersionTimeout(t, fault == "version-timeout")
			resolvedBinary, err := filepath.EvalSymlinks(newBinary)
			if err != nil {
				t.Fatal(err)
			}
			s := startVersionMode(t, nil, false, oldBinary, oldBinary)
			waitForUpgradeScreen(s, "old client", 15*time.Second, func(text string) bool { return strings.Contains(text, "ikesu v"+oldVersion) })
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
					if err := os.WriteFile(newBinary, []byte("#!/bin/sh\nkill -STOP $$\nexit 1\n"), 0700); err != nil {
						t.Fatal(err)
					}
				}
			}
			signalUpgradeTestProcess(t, pid, oldBinary, "CONT")
			stopped = false
			if fault == "version-timeout" {
				// Verify that the real --version child stopped itself.
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
							if len(fields) >= 4 && fields[0] == strconv.Itoa(pid) && fields[1] == "/bin/sh" && fields[2] == resolvedBinary && fields[3] == "--version" {
								child = candidate
								break
							}
						}
					}
				}
				waitForUpgradeTestStop(t, child, deadline.C)
				s.send("printf 'INPUT_DURING_VERSION_CHECK\\n'\r")
				waitForUpgradeScreen(s, "input during version check", 5*time.Second, func(text string) bool { return strings.Contains(text, "INPUT_DURING_VERSION_CHECK") })
			}
			waitForUpgradeScreen(s, "upgrade failure reason", 15*time.Second, func(text string) bool { return strings.Contains(text, "upgrade:") })
			if fault == "version-timeout" && !strings.Contains(s.text(), "upgrade: version timeout") {
				t.Fatalf("timeout reason was not visible:\n%s", s.text())
			}
			if fault == "broken-link" && !strings.Contains(s.text(), "upgrade: file missing") {
				t.Fatalf("missing file reason was not visible:\n%s", s.text())
			}
			if bytes.Contains(output.Bytes(), []byte(tui.LeaveSeq)) {
				t.Fatal("rejected candidate closed the old terminal")
			}
			s.send("printf 'INPUT_AFTER_UPGRADE_REJECTION\\n'\r")
			waitForUpgradeScreen(s, "input after rejection", 5*time.Second, func(text string) bool { return strings.Contains(text, "INPUT_AFTER_UPGRADE_REJECTION") })
		})
	}
}

// Real client builds and an unavailable server can exceed five seconds under load.
func TestClientSurvivesUnavailableServerAfterUpgradeRejection(t *testing.T) {
	t.Parallel()
	dir, oldBinary, newBinary, oldVersion, _ := buildStoppedClientUpgradeVersions(t, true)
	s := startVersionMode(t, nil, false, oldBinary, oldBinary)
	s.waitFor("old client", 15*time.Second, func(text string) bool { return strings.Contains(text, "ikesu v"+oldVersion) })
	if out, err := s.cli("server", "live-handoff", "--import-exe", newBinary); err != nil {
		t.Fatalf("handoff: %v %s", err, out)
	}
	var output bytes.Buffer
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	for !bytes.Contains(output.Bytes(), []byte(tui.MouseOffSeq)) {
		select {
		case data, ok := <-s.buf.ch:
			if !ok {
				t.Fatal("client exited before upgrade rejection")
			}
			output.Write(data)
			_, _ = s.emu.Write(data)
		case <-timer.C:
			t.Fatal("client did not reach terminal drain")
		}
	}
	pid := s.cmd.Process.Pid
	waitForUpgradeTestStop(t, pid, timer.C)
	stopped := true
	defer func() {
		if stopped {
			signalUpgradeTestProcess(t, pid, oldBinary, "CONT")
		}
	}()
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
	socket := filepath.Join(s.stateDir, s.name, "ikesu-client.sock")
	held := socket + ".held"
	if err := os.Rename(socket, held); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Rename(held, socket) }()
	signalUpgradeTestProcess(t, pid, oldBinary, "CONT")
	stopped = false
	restored := time.NewTimer(5 * time.Second)
	defer restored.Stop()
	for !bytes.Contains(output.Bytes(), []byte(tui.EnterSeq)) {
		select {
		case data, ok := <-s.buf.ch:
			if !ok {
				t.Fatal("client exited before screen was restored")
			}
			output.Write(data)
			_, _ = s.emu.Write(data)
		case <-restored.C:
			t.Fatal("client did not restore the screen")
		}
	}
	// Keep the socket unavailable beyond the test binary's 250 ms attach timeout.
	deadline := time.NewTimer(300 * time.Millisecond)
	defer deadline.Stop()
	for {
		select {
		case data, ok := <-s.buf.ch:
			if !ok {
				t.Fatal("client exited while server was unavailable")
			}
			_, _ = s.emu.Write(data)
		case <-deadline.C:
			if err := os.Rename(held, socket); err != nil {
				t.Fatal(err)
			}
			s.send("printf 'RECOVERED_AFTER_UNAVAILABLE\\n'\r")
			s.waitFor("recovered input", 15*time.Second, func(text string) bool { return strings.Contains(text, "RECOVERED_AFTER_UNAVAILABLE") })
			var read api.PaneReadResultEnvelope
			s.rpc(api.MethodPaneRead, api.PaneReadParams{PaneID: "p1", StripANSI: true, Source: "recent", Format: "text"}, &read)
			if !strings.Contains(read.Read.Text, "RECOVERED_AFTER_UNAVAILABLE") {
				t.Fatalf("input missing from pane: %s", read.Read.Text)
			}
			return
		}
	}
}

// Closing the real PTY master produces a terminal cleanup error after normal EOF.
func TestClientNormalDetachIgnoresTerminalCleanupError(t *testing.T) {
	s := start(t)
	s.waitFor("server ready", 10*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 24, Cols: 80}); err != nil {
		t.Fatal(err)
	}
	output, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	errorOutput, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer errorOutput.Close()
	cmd := exec.Command(binPath, "--session", s.name)
	cmd.Env = s.cmd.Env
	cmd.Stdin = slave
	cmd.Stdout = output
	cmd.Stderr = errorOutput
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		data, err := os.ReadFile(output.Name())
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(tui.EnterSeq)) {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("client exited before attach: %v", err)
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("client did not attach")
		}
	}
	if err := master.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		data, readErr := os.ReadFile(errorOutput.Name())
		if readErr != nil {
			t.Fatal(readErr)
		}
		if err != nil || len(data) != 0 {
			t.Fatalf("normal detach reported cleanup error: exit=%v stderr=%q", err, data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("client did not detach after PTY closed")
	}
}
