package e2e

import (
	"bufio"
	"encoding/json"
	"github.com/kazuph/ikesu/internal/api"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func migrationRPC(t *testing.T, socket, method string, params any, out any) {
	t.Helper()
	conn, err := net.DialTimeout("unix", socket, 5*time.Second)
	if err != nil {
		t.Fatal(method, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	if err = json.NewEncoder(conn).Encode(map[string]any{"id": "migration", "method": method, "params": params}); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Result json.RawMessage `json:"result"`
		Error  *api.Error      `json:"error"`
	}
	if err = json.NewDecoder(bufio.NewReader(conn)).Decode(&response); err != nil {
		t.Fatal(method, err)
	}
	if response.Error != nil {
		t.Fatal(method, response.Error)
	}
	if out != nil {
		if err = json.Unmarshal(response.Result, out); err != nil {
			t.Fatal(method, err, string(response.Result))
		}
	}
}

func migrationEnv(home string) []string {
	var env []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(key, "IKESU") || strings.HasPrefix(key, "HERDR") || key == "HOME" || key == "XDG_CONFIG_HOME" || key == "ZDOTDIR" || key == "SHELL" {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "SHELL=/bin/sh", "TERM=xterm-256color")
}

func migrationServer(t *testing.T, binary, home, socket string) func() {
	t.Helper()
	logPath := filepath.Join(home, filepath.Base(binary)+".server.log")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "--session", "stage3", "server")
	cmd.Env = migrationEnv(home)
	cmd.Stdout = log
	cmd.Stderr = log
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		if _, err := os.Stat(socket); err == nil {
			migrationRPC(t, socket, "server.stop", map[string]any{}, nil)
		} else {
			_ = cmd.Process.Kill()
		}
		select {
		case err := <-done:
			if err != nil {
				t.Log("server exit", err)
			}
		case <-time.After(15 * time.Second):
			_ = cmd.Process.Kill()
			<-done
			t.Error("server stop timed out")
		}
		_ = log.Close()
	}
	t.Cleanup(stop)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := net.Dial("unix", socket); err == nil {
			_ = conn.Close()
			return stop
		}
		select {
		case err := <-done:
			stopped = true
			_ = log.Close()
			raw, _ := os.ReadFile(logPath)
			t.Fatalf("server exited %v: %s", err, raw)
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	raw, _ := os.ReadFile(logPath)
	t.Fatalf("server not ready: %s", raw)
	return stop
}
