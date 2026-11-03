//go:build darwin

package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

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

// The timeout case must wait for the real client's ten-second version deadline.
func TestClientKeepsRunningOnRejectedUpgradeExecutables(t *testing.T) {
	for _, fault := range []string{"not-executable", "broken-link", "version-timeout"} {
		t.Run(fault, func(t *testing.T) {
			dir, oldBinary, newBinary, oldVersion, _ := buildClientUpgradeVersions(t)
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
						t.Fatal("version process did not start")
					case <-ticker.C:
						out, _ := exec.Command("pgrep", "-u", strconv.Itoa(os.Getuid()), "-f", newBinary).Output()
						for _, field := range strings.Fields(string(out)) {
							candidate, err := strconv.Atoi(field)
							if err != nil {
								continue
							}
							info, _ := exec.Command("ps", "-p", field, "-o", "ppid=", "-o", "command=").Output()
							fields := strings.Fields(string(info))
							if len(fields) >= 3 && fields[0] == strconv.Itoa(pid) && fields[1] == newBinary && fields[2] == "--version" {
								child = candidate
								break
							}
						}
					}
				}
				signalUpgradeTestProcess(t, child, newBinary, "STOP")
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
