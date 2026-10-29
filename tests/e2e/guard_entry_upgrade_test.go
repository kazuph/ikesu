package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

func TestGuardEntryUpgradeKeepsClaudePane(t *testing.T) {
	t.Parallel()
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\\''") + "'" }
	// The optional baseline is built from origin/main on mini. Without it this
	// also proves the normal same-version handoff path in the complete suite.
	old := os.Getenv("GUARD_ENTRY_BASELINE_BINARY")
	if old == "" {
		old = binPath
	}
	s := startVersionMode(t, nil, false, old, "")
	s.waitFor("prompt", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	dir := filepath.Join(s.stateDir, "guard", "shell", "p_1")
	prefix := filepath.Join(dir, "claude-prefix")
	// A real long-lived pane process launches the inherited prefix for each
	// command, as Claude does. No agent implementation or authorization is mocked.
	result := filepath.Join(s.stateDir, "result")
	launcher := "printf 'READY_%s\\n' GUARD; while read gate; do " + quote(prefix) + " " + quote("/bin/bash -c 'printf next' > "+quote(result)) + "; printf 'FINISHED_%s\\n' \"$gate\"; done"
	s.send("/bin/sh -c " + quote(launcher) + "\r")
	s.waitFor("persistent launcher", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "READY_GUARD") })
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "entry-upgrade-test", Agent: "claude", State: "idle"}, nil)
	// Native inode evidence distinguishes current executable from the old one
	// even when the production version string is deliberately not bumped.
	next := filepath.Join(t.TempDir(), "ikesu-next")
	data, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(next, data, 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := s.cli("server", "live-handoff", "--import-exe", next); err != nil {
		t.Fatal(out, err)
	}
	current, err := os.Stat(filepath.Join(dir, "ikesu"))
	expected, _ := os.Stat(next)
	if err != nil || !os.SameFile(current, expected) {
		t.Fatal("entry did not switch", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "bash-env.current"))
	if err != nil || !strings.Contains(string(body), next) {
		t.Fatal("generator did not switch", err)
	}
	if strings.Contains(string(body), "'"+old+"' guard") {
		t.Fatal("payload retains old executable")
	}
	s.send("go\r")
	s.waitFor("next Claude command", 5*time.Second, func(string) bool { b, e := os.ReadFile(result); return e == nil && string(b) == "next" })
	// Missing/corrupt fixed entry must stop before the requested command writes.
	for _, corrupt := range []bool{false, true} {
		if err := os.Remove(filepath.Join(dir, "ikesu")); err != nil {
			t.Fatal(err)
		}
		if corrupt {
			if err := os.WriteFile(filepath.Join(dir, "ikesu"), []byte("invalid executable\n"), 0700); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Remove(result); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		gate := "missing"
		if corrupt {
			gate = "corrupt"
		}
		s.send(gate + "\r")
		s.waitFor("entry failed closed", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "FINISHED_"+gate) })
		if _, err := os.Stat(result); err == nil {
			// The server can repair the entry before this invocation reaches it.
			// Execution is valid only through the original trusted native inode.
			repaired, e := os.Stat(filepath.Join(dir, "ikesu"))
			if e != nil || !os.SameFile(repaired, expected) {
				t.Fatal("execution bypassed trusted runtime", e)
			}
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(dir, "ikesu")); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if err := os.Symlink(next, filepath.Join(dir, "ikesu")); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("persistent Claude-like pane used native %s; source=%s; missing/corrupt entry stopped execution", next, old)
}

func TestGuardDisabledPaneSurvivesHandoffWithoutAssets(t *testing.T) {
	t.Parallel()
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte("onboarding=false\n[guard]\nenabled=false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := startEnv(t, map[string]string{"IKESU_CONFIG": cfg})
	s.waitFor("prompt", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	pidfile := filepath.Join(s.stateDir, "shell-pid")
	s.send("printf '%s' $$ > '" + pidfile + "'\r")
	s.waitFor("pid saved", 5*time.Second, func(string) bool { _, err := os.Stat(pidfile); return err == nil })
	before, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := s.cli("server", "live-handoff", "--import-exe", binPath); err != nil {
		t.Fatal(out, err)
	}
	s.send("printf '%s' $$ > '" + pidfile + "'; printf 'DISABLED_%s\\n' ALIVE\r")
	s.waitFor("same pane accepts input", 5*time.Second, func(text string) bool { return strings.Contains(text, "DISABLED_ALIVE") })
	after, err := os.ReadFile(pidfile)
	if err != nil || string(before) != string(after) {
		t.Fatalf("shell changed: %s -> %s: %v", before, after, err)
	}
	if _, err := os.Lstat(filepath.Join(s.stateDir, "guard", "shell")); !os.IsNotExist(err) {
		t.Fatalf("disabled pane gained startup files: %v", err)
	}
}

func TestGuardGenerationRollbackAfterRealHandoffFailure(t *testing.T) {
	t.Parallel()
	source := os.Getenv("GUARD_ENTRY_BASELINE_BINARY")
	if source == "" {
		source = binPath
	}
	old := filepath.Join(t.TempDir(), "ikesu")
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, data, 0700); err != nil {
		t.Fatal(err)
	}
	s := startVersionMode(t, nil, false, old, "")
	s.waitFor("old prompt", 5*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	first := "p1"
	var split api.PaneInfoResult
	s.rpc(api.MethodPaneSplit, api.PaneSplitParams{TargetPaneID: &first, Direction: "right", Focus: false}, &split)
	for _, pane := range []string{"p1", split.Pane.PaneID} {
		s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: pane, Source: "rollback-test", Agent: "codex", State: "idle"}, nil)
	}
	marker := filepath.Join(s.stateDir, "guard", "agent-panes", "p_1.json")
	s.waitFor("guard armed", 5*time.Second, func(string) bool { _, err := os.Stat(marker); return err == nil })
	secondDir := filepath.Join(s.stateDir, "guard", "shell", strings.Replace(split.Pane.PaneID, "p", "p_", 1))
	bad := filepath.Join(secondDir, "bash-env.current")
	if err := os.Remove(bad); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Mkdir(bad, 0700); err != nil {
		t.Fatal(err)
	}
	paths := []string{marker, filepath.Join(s.stateDir, "guard", "shell", "p_1", "claude-prefix"), filepath.Join(s.stateDir, "guard", "shell", "p_1", "bash-env")}
	before := map[string][]byte{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = data
	}
	if out, err := s.cli("server", "live-handoff", "--import-exe", binPath); err == nil || !strings.Contains(out, "handoff_failed") {
		t.Fatalf("generation failure did not abort handoff: %v %s", err, out)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != string(before[path]) {
			t.Fatalf("rollback changed %s: %v", path, err)
		}
	}
	s.send("printf 'ROLLBACK_%s\\n' ALIVE\r")
	s.waitFor("old pane input after rollback", 5*time.Second, func(text string) bool { return strings.Contains(text, "ROLLBACK_ALIVE") })
	// Approval still has to be bound to the original native executable. A real
	// guarded deletion must complete after approval on the surviving old server.
	outside := t.TempDir()
	home := filepath.Join(outside, "home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	target := home + "/../victim"
	if err := os.WriteFile(target, []byte("disposable"), 0600); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(s.stateDir, "guard", "bin", "p_1", "rm")
	cmd := exec.Command(shim, target)
	cmd.Env = []string{"HOME=" + outside, "PATH=/usr/bin:/bin", "AI_GUARD_REQUEST_REASON=対象プロジェクト: ikesu隔離試験。必要な理由: 引き継ぎ失敗後も旧判定の承認bindingを確認。実施内容: 使い捨てファイルを削除する。"}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	finished := false
	t.Cleanup(func() {
		if !finished {
			cmd.Process.Kill()
			<-done
		}
	})
	var pending api.DecisionListResult
	s.waitFor("old native approval pending", 5*time.Second, func(string) bool {
		s.rpc(api.MethodDecisionList, api.DecisionListParams{Status: "pending"}, &pending)
		return len(pending.Decisions) == 1
	})
	if out, err := s.cli("decision", "answer", pending.Decisions[0].DecisionID, "approve"); err != nil {
		t.Fatal(out, err)
	}
	select {
	case err := <-done:
		finished = true
		if err != nil {
			t.Fatal(err, output.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("old native approval binding was not restored")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("approved deletion did not execute: %v %s", err, output.String())
	}
	t.Logf("original native %s retained prefix, startup and approval binding after failed import", old)
}
