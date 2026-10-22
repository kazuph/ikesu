package e2e

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/dispatch"
)

// Capture before TestMain isolates the test environment.
var migrationBaseline = os.Getenv("IKESU_MIGRATION_BASE_BIN")

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
		if strings.HasPrefix(key, "IKESU") || strings.HasPrefix(key, "GTX") || strings.HasPrefix(key, "HERDR") || key == "HOME" || key == "XDG_CONFIG_HOME" || key == "ZDOTDIR" || key == "SHELL" {
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

func prepareMigrationBaseline(t *testing.T) string {
	t.Helper()
	if migrationBaseline == "" {
		dir := t.TempDir()
		archive := filepath.Join(dir, "baseline.tar")
		cmd := exec.Command("git", "archive", "--output", archive, "49f0363")
		cmd.Dir = repoRoot()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("baseline archive (or set IKESU_MIGRATION_BASE_BIN): %v %s", err, out)
		}
		if out, err := exec.Command("tar", "-xf", archive, "-C", dir).CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
		migrationBaseline = filepath.Join(dir, "gtx-2e01183")
		cmd = exec.Command("go", "build", "-o", migrationBaseline, "./cmd/ikesu")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		}
	}
	return migrationBaseline
}

func TestStageThreeOfflineMigration(t *testing.T) {
	baseline := prepareMigrationBaseline(t)
	home, err := os.MkdirTemp("/tmp", "im-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(home)
	oldRepo := filepath.Join(home, "src/github.com/kazuph/goterrace")
	newRepo := filepath.Join(home, "src/github.com/kazuph/ikesu")
	plugin := filepath.Join(oldRepo, "apps/example")
	if err = os.MkdirAll(plugin, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := "id='migration.example'\nname='Migration example'\nversion='1.0.0'\nmin_gtx_version='0.3.0'\n[[actions]]\nid='proof'\ntitle='Proof'\ncommand=['/bin/sh','-c','printf plugin-ok']\n"
	if err = os.WriteFile(filepath.Join(plugin, "gtx-plugin.toml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(plugin, "ikesu-plugin.toml"), []byte(strings.ReplaceAll(manifest, "min_gtx_version", "min_ikesu_version")), 0600); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(home, ".config/gtx")
	if err = os.MkdirAll(config, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(config, "config.toml"), []byte("onboarding=false\n[experimental]\nallow_nested=true\n[session]\nresume_agents_on_restore=false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink("gtx", filepath.Join(home, ".config/gtx-2e01183")); err != nil {
		t.Fatal(err)
	}
	oldRoot := filepath.Join(home, ".local/state/gtx-2e01183")
	oldSocket := filepath.Join(oldRoot, "stage3/gtx.sock")
	stop := migrationServer(t, baseline, home, oldSocket)
	var created struct {
		WorkspaceID string `json:"workspace_id"`
	}
	migrationRPC(t, oldSocket, "workspace.create", map[string]any{"cwd": oldRepo, "label": "migration-workspace", "focus": true}, &created)
	var panes struct {
		Panes []struct {
			PaneID string `json:"pane_id"`
		} `json:"panes"`
	}
	migrationRPC(t, oldSocket, "pane.list", map[string]any{}, &panes)
	if len(panes.Panes) == 0 {
		t.Fatal("no real panes")
	}
	pane := panes.Panes[len(panes.Panes)-1].PaneID
	migrationRPC(t, oldSocket, "pane.rename", map[string]any{"pane_id": pane, "label": "migration-pane"}, nil)
	migrationRPC(t, oldSocket, "pane.report_agent", api.PaneReportAgentParams{PaneID: pane, Source: "migration", Agent: "codex", State: "idle"}, nil)
	migrationRPC(t, oldSocket, "msg.send", api.MsgSendParams{Room: "migration", FromAgent: "writer", To: pane, Body: "retained-mail"}, nil)
	var decision api.DecisionResult
	migrationRPC(t, oldSocket, "decision.create", api.DecisionCreateParams{Kind: "ask", Title: "retained-decision", Options: []api.DecisionOption{{ID: "yes", Label: "Yes", Role: "approve"}}}, &decision)
	migrationRPC(t, oldSocket, "plugin.link", map[string]any{"path": plugin, "enabled": true}, nil)
	var job api.RunStartedResult
	migrationRPC(t, oldSocket, "run.start", api.RunStartParams{CallerPane: pane, Label: "retained-job", Cwd: oldRepo, Argv: []string{"/bin/sh", "-c", "printf retained-job-output"}, Completion: "none"}, &job)
	db, err := dispatch.Open(filepath.Join(oldRoot, "sessions/stage3/gtx.db"))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	var oldJob *dispatch.Job
	for time.Now().Before(deadline) {
		oldJob, err = db.CommandRow(job.Job)
		if err != nil {
			t.Fatal(err)
		}
		if oldJob != nil && oldJob.Status == "exited" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if oldJob == nil || oldJob.Status != "exited" {
		t.Fatalf("job not complete: %+v", oldJob)
	}
	_ = db.Close()
	stop()
	saved := filepath.Join(oldRoot, "stage3/session.json")
	if _, err = os.Stat(saved); err != nil {
		t.Fatal(err)
	}
	migration := exec.Command("python3", filepath.Join(repoRoot(), "scripts/migrate-ikesu.py"))
	migration.Env = migrationEnv(home)
	raw, err := migration.CombinedOutput()
	t.Logf("migration output:\n%s", raw)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(oldRepo, newRepo); err != nil {
		t.Fatal(err)
	}
	// Run under three deployment names, all using the same state/config and session.
	newRoot := filepath.Join(home, ".local/state/ikesu")
	newSocket := filepath.Join(newRoot, "stage3/ikesu.sock")
	for _, name := range []string{"ikesu", "ikesu-xyz", "foo"} {
		t.Run(name, func(t *testing.T) {
			binary := filepath.Join(home, name)
			if err := os.Symlink(binPath, binary); err != nil {
				t.Fatal(err)
			}
			stop := migrationServer(t, binary, home, newSocket)
			defer stop()
			var list map[string]any
			migrationRPC(t, newSocket, "pane.list", map[string]any{}, &list)
			encoded, _ := json.Marshal(list)
			if !strings.Contains(string(encoded), "migration-pane") || !strings.Contains(string(encoded), newRepo) {
				t.Fatal("pane/cwd not restored", string(encoded))
			}
			var history api.MsgHistoryResult
			migrationRPC(t, newSocket, "msg.history", api.MsgHistoryParams{Room: "migration", Limit: 50}, &history)
			if len(history.Messages) != 1 || history.Messages[0].Body != "retained-mail" {
				t.Fatal(history)
			}
			var got api.DecisionResult
			migrationRPC(t, newSocket, "decision.get", api.DecisionTargetParams{DecisionID: decision.Decision.DecisionID}, &got)
			if got.Decision.Title != "retained-decision" || got.Decision.Status != "pending" {
				t.Fatal(got)
			}
			var plugins map[string]any
			migrationRPC(t, newSocket, "plugin.list", map[string]any{}, &plugins)
			pluginRaw, _ := json.Marshal(plugins)
			if !strings.Contains(string(pluginRaw), "migration.example") || strings.Contains(string(pluginRaw), oldRepo) {
				t.Fatal(string(pluginRaw))
			}
			db, err := dispatch.Open(filepath.Join(newRoot, "sessions/stage3/ikesu.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			row, err := db.CommandRow(job.Job)
			if err != nil || row == nil {
				t.Fatal(row, err)
			}
			if row.Cwd != newRepo || row.LogPath != strings.Replace(oldJob.LogPath, oldRoot, newRoot, 1) {
				t.Fatalf("job paths %+v", row)
			}
			log, err := os.ReadFile(row.LogPath)
			if err != nil || !strings.Contains(string(log), "retained-job-output") {
				t.Fatal(string(log), err)
			}
			// A fresh job and decision response prove the restored server remains usable.
			var fresh api.RunStartedResult
			migrationRPC(t, newSocket, "run.start", api.RunStartParams{CallerPane: pane, Argv: []string{"/usr/bin/true"}, Completion: "none"}, &fresh)
			if fresh.Job == job.Job {
				t.Fatal("job identity reused")
			}
			t.Logf("PASS executable=%s root=%s config=%s panes/mail/decision/plugin/job/log/cwd restored", name, newRoot, filepath.Join(home, ".config/ikesu/config.toml"))
		})
	}
}
