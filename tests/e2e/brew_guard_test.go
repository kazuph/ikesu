package e2e

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWritableInstallStartsRetainedServer(t *testing.T) {
	install, err := os.MkdirTemp("/tmp", "ikesu-brew-install-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(install) })
	if err := os.Chmod(install, 0775); err != nil {
		t.Fatal(err)
	}
	home, err := os.MkdirTemp("/tmp", "ikesu-brew-home-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	state, err := os.MkdirTemp("/tmp", "ikesu-brew-state-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(state) })
	data, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(install, "ikesu")
	if err = os.WriteFile(binary, data, 0755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(home, "config.toml")
	if err = os.WriteFile(config, []byte("onboarding=false\n[experimental]\nallow_nested=true\n[session]\nresume_agents_on_restore=false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "HOME="+home, "IKESU_STATE_DIR="+state, "IKESU_CONFIG="+config, "SHELL=/bin/sh")
	server := exec.Command(binary, "--session", "brew-test", "server")
	server.Env = env
	log, err := os.Create(filepath.Join(home, "server-output"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	server.Stdout, server.Stderr = log, log
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	cli := func(args ...string) (string, error) {
		c := exec.Command(binPath, append([]string{"--session", "brew-test"}, args...)...)
		c.Env = env
		out, e := c.CombinedOutput()
		return string(out), e
	}
	t.Cleanup(func() { _, _ = cli("server", "stop"); _ = server.Wait() })
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := cli("server", "ping"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			b, _ := os.ReadFile(log.Name())
			t.Fatalf("server failed to start: %s", b)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if out, err := cli("pane", "run", "p1", "printf 'brew_shell_%s\\n' works"); err != nil {
		t.Fatalf("run: %v %s", err, out)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		out, err := cli("pane", "read", "p1")
		if err == nil && strings.Contains(out, "brew_shell_works") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("shell did not respond: %v %s", err, out)
		}
		time.Sleep(20 * time.Millisecond)
	}
	matches, err := filepath.Glob(filepath.Join(home, ".local/lib/ikesu/releases/*/ikesu"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("retained executable: %v %v", matches, err)
	}
	out, err := exec.Command("lsof", "-a", "-p", fmt.Sprint(server.Process.Pid), "-d", "txt", "-Fn").Output()
	physical, resolveErr := filepath.EvalSymlinks(matches[0])
	if err != nil || resolveErr != nil || !strings.Contains(string(out), "n"+physical+"\n") {
		t.Fatalf("server executable = %q, want %s: %v", out, matches[0], err)
	}

	// A new build at the original path must be selected on restart.
	updated := append(append([]byte(nil), data...), []byte("new-build")...)
	newBinary := filepath.Join(install, "new-ikesu")
	if err := os.WriteFile(newBinary, updated, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(newBinary, binary); err != nil {
		t.Fatal(err)
	}
	if out, err := cli("server", "restart"); err != nil {
		t.Fatalf("restart with updated executable: %v %s", err, out)
	}
	sum := sha256.Sum256(updated)
	version := filepath.Base(filepath.Dir(matches[0]))
	hashed := filepath.Join(home, ".local/lib/ikesu/releases", version+fmt.Sprintf("+%x", sum)[:13], "ikesu")
	if got, err := os.ReadFile(hashed); err != nil || string(got) != string(updated) {
		logData, _ := os.ReadFile(filepath.Join(state, "brew-test", "server.log"))
		found, _ := filepath.Glob(filepath.Join(home, ".local/lib/ikesu/releases/*/ikesu"))
		t.Fatalf("updated build not retained: %s %v; releases=%v; log=%s", hashed, err, found, logData)
	}
	if original, err := os.ReadFile(matches[0]); err != nil || string(original) != string(data) {
		t.Fatalf("original retained build changed: %v", err)
	}
	if err := os.Remove(binary); err != nil {
		t.Fatal(err)
	}
	if out, err := cli("server", "restart"); err != nil {
		t.Fatalf("restart without original executable: %v %s", err, out)
	}
	logData, err := os.ReadFile(filepath.Join(state, "brew-test", "server.log"))
	if err != nil || !strings.Contains(string(logData), "restart executable: "+binary) || !strings.Contains(string(logData), "restart executable: "+hashed) {
		t.Fatalf("restart executable choices absent: %s %v", logData, err)
	}
}

func TestWritableInstallStandalone(t *testing.T) {
	install, err := os.MkdirTemp("/tmp", "ikesu-brew-standalone-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(install) })
	if err := os.Chmod(install, 0775); err != nil {
		t.Fatal(err)
	}
	home, err := os.MkdirTemp("/tmp", "ikesu-brew-home-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	data, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(install, "ikesu")
	if err := os.WriteFile(binary, data, 0755); err != nil {
		t.Fatal(err)
	}
	s := startVersionMode(t, map[string]string{"HOME": home}, true, binary, "")
	s.waitFor("standalone shell", 10*time.Second, func(screen string) bool { return strings.Contains(screen, "❯") })
	s.send("printf 'standalone_shell_%s\\n' works\r")
	s.waitFor("standalone output", 5*time.Second, func(screen string) bool { return strings.Contains(screen, "standalone_shell_works") })
	matches, err := filepath.Glob(filepath.Join(home, ".local/lib/ikesu/releases/*/ikesu"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("retained executable: %v %v", matches, err)
	}
	physical, err := filepath.EvalSymlinks(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("lsof", "-a", "-p", fmt.Sprint(s.cmd.Process.Pid), "-d", "txt", "-Fn").Output()
	if err != nil || !strings.Contains(string(out), "n"+physical+"\n") {
		t.Fatalf("standalone executable: %q, want %s: %v", out, physical, err)
	}
}
