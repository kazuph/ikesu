//go:build darwin && signed_app_integration

package distribution

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/plugin"
)

func TestEnsureActionReportsOwnedRunningVersion(t *testing.T) {
	app := os.Getenv("IKESU_SIGNED_DIALOG_APP")
	if info, err := os.Stat(app); err != nil || !info.IsDir() {
		t.Fatalf("IKESU_SIGNED_DIALOG_APP must name a real signed panel app: %v", err)
	}
	node := fixtureNode(t)
	work, err := os.MkdirTemp("/tmp", "ikesu-node-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(work)
	work, err = filepath.EvalSymlinks(work)
	if err != nil {
		t.Fatal(err)
	}
	repository, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(work, "ikesu")
	build := fixtureCommand(t, os.Environ(), "go", "build", "-o", binary, "./cmd/ikesu")
	build.Dir = repository
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	root := filepath.Join(work, "release")
	if err := os.CopyFS(root, os.DirFS(filepath.Join(repository, "apps/mac-dialog-decisions"))); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(app, filepath.Join(root, "IkesuDecisionDialog.app")); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "ikesu-plugin.toml")
	m, _, err := plugin.ReadManifest(manifest, "0.4.4")
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"VERSION": m.Version + "\n", "CHANNEL": "release\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	config := filepath.Join(work, "config.toml")
	if err := os.WriteFile(config, []byte("onboarding=false\n[experimental]\nallow_nested=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(work, "state")
	env := fixtureEnvironment(work, "PATH="+filepath.Dir(node)+":/usr/bin:/bin:/usr/sbin:/sbin", "IKESU_CONFIG="+config,
		"IKESU_CONFIG_PATH="+config, "IKESU_STATE_DIR="+state, "IKESU_SOCKET_PATH="+api.APISocketPath(filepath.Join(state, "default")),
		"IKESU_SOCKET_PATH_EXPLICIT=1", "IKESU_DB_PATH="+filepath.Join(work, "dispatch.db"))
	cli := func(args ...string) ([]byte, error) {
		cmd := fixtureCommand(t, env, append([]string{binary}, args...)...)
		cmd.Dir = work
		return cmd.CombinedOutput()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*fixtureWait)
	defer cancel()
	log, err := os.Create(filepath.Join(work, "server.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	server := exec.CommandContext(ctx, binary, "server")
	server.Env, server.Dir, server.Stdout, server.Stderr = env, work, log, log
	server.WaitDelay = fixtureWait
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Wait() }()
	ownerFile := filepath.Join(state, "plugins", m.ID, "daemon.json")
	defer func() {
		var owner struct{ PID int }
		if data, err := os.ReadFile(ownerFile); err == nil && json.Unmarshal(data, &owner) == nil && owner.PID > 0 {
			pids, err := exec.Command("pgrep", "-u", strconv.Itoa(os.Getuid()), "-f", work).Output()
			if err != nil || !strings.Contains("\n"+string(pids), "\n"+strconv.Itoa(owner.PID)+"\n") {
				t.Errorf("cannot prove own daemon PID %d: %v", owner.PID, err)
			} else if err := syscall.Kill(owner.PID, syscall.SIGTERM); err != nil {
				t.Error(err)
			}
		}
		if out, err := cli("server", "stop"); err != nil {
			t.Errorf("stop: %v: %s", err, out)
			cancel()
		}
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(fixtureWait):
			t.Error("server exit deadline")
			cancel()
		}
	}()
	deadline := time.Now().Add(fixtureWait)
	for {
		conn, err := net.DialTimeout("unix", api.APISocketPath(filepath.Join(state, "default")), fixtureWait)
		if err == nil {
			conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("server readiness deadline", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, args := range [][]string{{"plugin", "link", manifest}, {"plugin", "action", "invoke", "start", "--plugin", m.ID}} {
		if out, err := cli(args...); err != nil {
			t.Fatalf("%v: %v: %s", args, err, out)
		}
	}
	var owner struct {
		PID           int
		Owner, Script string
	}
	var running struct {
		PID            int
		Owner, Version string
	}
	deadline = time.Now().Add(3 * fixtureWait)
	for {
		data, e1 := os.ReadFile(ownerFile)
		stateData, e2 := os.ReadFile(filepath.Join(filepath.Dir(ownerFile), "daemon-state.json"))
		if e1 == nil && e2 == nil && json.Unmarshal(data, &owner) == nil && json.Unmarshal(stateData, &running) == nil && owner.PID > 0 && owner.PID == running.PID && owner.Owner == running.Owner {
			break
		}
		if time.Now().After(deadline) {
			out, _ := cli("log", "j0001", "--tail", "0")
			t.Fatalf("daemon readiness deadline: %s", out)
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Readiness stays local; Status is the real implementation before the CLI's
	// remote latest lookup, so this regression never polls the public API.
	i := Installer{Product: MacDialog, Home: work, Store: &plugin.Store{ConfigRoot: work, StateRoot: state, Version: "0.4.4"}}
	status, err := i.Status()
	if err != nil || status.RunningVersion != m.Version || status.RunningUnknown {
		command, _ := exec.Command("ps", "-o", "command=", "-p", fmt.Sprint(owner.PID)).Output()
		t.Fatalf("status=%+v err=%v ps=%s", status, err, command)
	}
	t.Logf("real plugin action -> ensure -> run: running_version=%s running_unknown=%v pid=%d", status.RunningVersion, status.RunningUnknown, owner.PID)
	proof := fixtureCommand(t, env, node, "--input-type=module", "-e",
		`import fs from 'node:fs'; const {ownerProven}=await import(process.argv[1]); const owner=JSON.parse(fs.readFileSync(process.argv[2])); console.log(JSON.stringify([ownerProven(owner,process.argv[3]),ownerProven({...owner,node:'node'},process.argv[3])]))`,
		filepath.Join(root, "lifecycle.mjs"), ownerFile, filepath.Dir(ownerFile))
	if out, err := proof.CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "[true,false]" {
		t.Fatalf("strict owner proof: %v: %s", err, out)
	}
}
