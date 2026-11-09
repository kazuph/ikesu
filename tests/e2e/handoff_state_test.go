package e2e

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	sessionfile "github.com/kazuph/ikesu/internal/session"
	"golang.org/x/sys/unix"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLiveHandoffRejectsDuplicateSavedCodexSessions(t *testing.T) {
	t.Parallel()
	cwd := t.TempDir()
	prepare := func(cmd *exec.Cmd) {
		var root string
		for _, entry := range cmd.Env {
			if strings.HasPrefix(entry, "IKESU_STATE_DIR=") {
				root = strings.TrimPrefix(entry, "IKESU_STATE_DIR=")
			}
		}
		if root == "" {
			t.Fatal("missing isolated state directory")
		}
		st := model.New(cwd, 80, 24)
		st.Apply(model.CmdSplitFocused{Dir: layout.Vertical})
		saved := st.Snapshot()
		saved.EachPane(func(_ *sessionfile.Workspace, _ *sessionfile.Tab, p *sessionfile.Pane) {
			p.Agent, p.SessionKind, p.SessionID = "codex", "id", "duplicate-session"
		})
		path := sessionfile.Path(filepath.Join(root, "e2e"))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := sessionfile.Save(path, saved); err != nil {
			t.Fatal(err)
		}
	}
	s := startVersionModePrepared(t, nil, false, binPath, "", prepare)
	s.waitFor("restored shell", 15*time.Second, func(screen string) bool { return strings.Contains(screen, "❯") })
	check := func() {
		t.Helper()
		var result api.AgentRestoreResult
		s.rpc(api.MethodAgentRestore, api.AgentRestoreParams{DryRun: true}, &result)
		if len(result.Actions) != 2 {
			t.Fatalf("restore actions: %+v", result.Actions)
		}
		for _, a := range result.Actions {
			if a.Status != "skipped" || a.Command != nil || a.Reason == nil || *a.Reason != "duplicate session id" {
				t.Fatalf("duplicate restore: %+v", a)
			}
		}
	}
	if out, err := s.cli("server", "live-handoff"); err != nil {
		t.Fatalf("handoff: %v %s", err, out)
	}
	check()
}

func TestLiveHandoffDrainsAcceptedInputOperations(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "input")
	ready := filepath.Join(dir, "ready")
	shell := filepath.Join(dir, "reader")
	script := fmt.Sprintf("#!/bin/sh\nstty echo; touch %q; while IFS= read -r line; do printf '%%s\\n' \"$line\" >> %q; done\n", ready, path)
	if err := os.WriteFile(shell, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	s := startEnv(t, map[string]string{"SHELL": shell})
	wait := func(want string) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			data, _ := os.ReadFile(path)
			if strings.Contains(string(data), want) {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		data, _ := os.ReadFile(path)
		t.Fatalf("input %q missing %q", data, want)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("reader not ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.rpc(api.MethodPaneSendInput, api.PaneSendInputParams{PaneID: "p1", Text: "first", Keys: []string{"Enter"}}, nil)
	s.waitFor("first input before Enter", 15*time.Second, func(screen string) bool { return strings.Contains(screen, "first") })
	s.rpc(api.MethodPaneSendInput, api.PaneSendInputParams{PaneID: "p1", Text: "second", Keys: []string{"Enter"}}, nil)
	if out, err := s.cli("server", "live-handoff"); err != nil {
		t.Fatalf("handoff: %v %s", err, out)
	}
	wait("first\nsecond\n")
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "first\nsecond\n" {
		t.Fatalf("input = %q: %v", data, err)
	}
}

func (s *session) rpc(method string, params, result any) {
	s.t.Helper()
	conn, err := net.Dial("unix", api.APISocketPath(filepath.Join(s.stateDir, s.name)))
	if err != nil {
		s.t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	if err = api.WriteRequest(conn, api.Request{ID: "handoff-proof", Method: method, Params: api.MarshalParams(params)}); err != nil {
		s.t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		s.t.Fatal(err)
	}
	var response api.Response
	if err = json.Unmarshal(line, &response); err != nil {
		s.t.Fatal(err)
	}
	if response.Err != nil {
		s.t.Fatalf("%s: %s", method, line)
	}
	if result != nil {
		if err = json.Unmarshal(response.Result, result); err != nil {
			s.t.Fatal(err)
		}
	}
}

func TestLiveHandoffDecisionsPluginsAgentAndGuard(t *testing.T) {
	t.Parallel()
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte("onboarding=false\n[session]\nresume_agents_on_restore=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := startEnv(t, map[string]string{"IKESU_CONFIG": cfg})
	s.waitFor("shell", 15*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	sessionID := "handoff-saved-conversation"
	lockDir := filepath.Join(os.Getenv("HOME"), ".codex", "thread-writer-locks")
	if err := os.MkdirAll(lockDir, 0700); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(lockDir, sessionID+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	seq := uint64(8)
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "handoff", Agent: "codex", State: "working", Seq: &seq, SessionID: &sessionID}, nil)
	marker := filepath.Join(s.stateDir, "guard", "agent-panes", "p_1.json")
	before, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	link := func(id string) {
		root := t.TempDir()
		manifest := fmt.Sprintf("id=%q\nname=%q\nversion=\"0.1.0\"\nmin_ikesu_version=\"0.1.0\"\nplatforms=[\"macos\",\"linux\"]\n[[panes]]\nid=\"board\"\ntitle=\"Board\"\ncommand=[\"/bin/cat\"]\n", id, id)
		if err := os.WriteFile(filepath.Join(root, "ikesu-plugin.toml"), []byte(manifest), 0600); err != nil {
			t.Fatal(err)
		}
		s.rpc("plugin.link", map[string]any{"path": root}, nil)
	}
	link("handoff.first")
	var opened struct {
		PluginPane struct {
			Pane api.PaneInfo `json:"pane"`
		} `json:"plugin_pane"`
	}
	s.rpc("plugin.pane.open", map[string]any{"plugin_id": "handoff.first", "entrypoint": "board", "placement": "split", "focus": false}, &opened)
	var pending, expiry api.DecisionResult
	options := []api.DecisionOption{{ID: "yes", Label: "Yes", Role: "approve"}}
	s.rpc(api.MethodDecisionCreate, api.DecisionCreateParams{Kind: "ask", Title: "Retained question", Options: options}, &pending)
	ms := uint64((10 * time.Second).Milliseconds())
	s.rpc(api.MethodDecisionCreate, api.DecisionCreateParams{Kind: "ask", Title: "Expiry continues", Options: options, TimeoutMS: &ms}, &expiry)
	if out, err := s.cli("server", "live-handoff"); err != nil {
		t.Fatalf("handoff: %v %s", err, out)
	}
	after, err := os.ReadFile(marker)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("guard marker changed: %s %s %v", before, after, err)
	}
	var info api.PaneInfoResult
	s.rpc(api.MethodPaneGet, map[string]any{"pane_id": "p1"}, &info)
	if info.Pane.Agent == nil || *info.Pane.Agent != "codex" || info.Pane.AgentStatus != "working" || info.Pane.AgentSession == nil || info.Pane.AgentSession.Value != sessionID {
		t.Fatalf("agent/session lost: %+v", info.Pane)
	}
	s.rpc("plugin.pane.focus", map[string]any{"pane_id": opened.PluginPane.Pane.PaneID}, nil)
	link("handoff.second")
	var registry struct {
		Plugins []struct {
			ID string `json:"plugin_id"`
		} `json:"plugins"`
	}
	s.rpc("plugin.list", map[string]any{}, &registry)
	ids := map[string]bool{}
	for _, item := range registry.Plugins {
		ids[item.ID] = true
	}
	if !ids["handoff.first"] || !ids["handoff.second"] {
		t.Fatalf("registry overwritten: %+v", registry)
	}
	var list api.DecisionListResult
	s.rpc(api.MethodDecisionList, api.DecisionListParams{}, &list)
	if len(list.Decisions) != 2 {
		t.Fatalf("questions duplicated/lost: %+v", list)
	}
	var retained api.DecisionResult
	s.rpc(api.MethodDecisionGet, api.DecisionTargetParams{DecisionID: pending.Decision.DecisionID}, &retained)
	if retained.Decision.Status != "pending" {
		t.Fatal(retained)
	}
	s.rpc(api.MethodDecisionWait, api.DecisionWaitParams{DecisionID: expiry.Decision.DecisionID}, &retained)
	if retained.Decision.Status != "expired" {
		t.Fatalf("expiry stopped: %+v", retained)
	}
	yes := "yes"
	s.rpc(api.MethodDecisionAnswer, api.DecisionAnswerParams{DecisionID: pending.Decision.DecisionID, OptionID: &yes}, &retained)
	if retained.Decision.Status != "answered" {
		t.Fatal(retained)
	}
	t.Logf("pending %s retained and answered; %s expired; plugins first+second; guard marker unchanged=%s", pending.Decision.DecisionID, expiry.Decision.DecisionID, after)
}

// A test-only build exercises the deadline without waiting 30 seconds.
func TestLiveHandoffUnresponsiveImporterRetainsPane(t *testing.T) {
	t.Parallel()
	buildDir := t.TempDir()
	sourcePath := filepath.Join(repoRoot(), "internal/server/handoff.go")
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	const original = "const handoffWait = 30 * time.Second"
	if strings.Count(string(source), original) != 1 {
		t.Fatal("handoff deadline declaration changed")
	}
	shortSource := filepath.Join(buildDir, "handoff.go")
	if err := os.WriteFile(shortSource, []byte(strings.Replace(string(source), original, "const handoffWait = time.Second", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	overlay, err := json.Marshal(map[string]map[string]string{"Replace": {sourcePath: shortSource}})
	if err != nil {
		t.Fatal(err)
	}
	overlayPath := filepath.Join(buildDir, "overlay.json")
	if err := os.WriteFile(overlayPath, overlay, 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(buildDir, "ikesu")
	build := exec.Command("go", "build", "-overlay", overlayPath, "-o", binary, "./cmd/ikesu")
	build.Dir = repoRoot()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build short-deadline test binary: %v %s", err, out)
	}
	s := startVersionMode(t, nil, false, binary, "")
	s.waitFor("shell", 15*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	root := t.TempDir()
	importer := filepath.Join(root, "importer")
	pidfile := filepath.Join(root, "pid")
	if err := os.WriteFile(importer, []byte(fmt.Sprintf("#!/bin/sh\nprintf '%%s' $$ > %q\nexec /bin/sleep 60\n", pidfile)), 0700); err != nil {
		t.Fatal(err)
	}
	out, err := s.cli("server", "live-handoff", "--import-exe", importer)
	if err == nil || !strings.Contains(out, "handoff_failed") || !strings.Contains(out, "timed out waiting for handoff import connection") {
		t.Fatalf("unresponsive importer: %s %v", out, err)
	}
	b, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(b))
	if err != nil {
		t.Fatal(err)
	}
	if unix.Kill(pid, 0) == nil {
		t.Fatalf("failed importer remains alive: %d", pid)
	}
	s.send("printf 'UNRESPONSIVE_HANDOFF_ALIVE\\n'\r")
	s.waitFor("old pane accepts input after timeout", 5*time.Second, func(tx string) bool { return strings.Contains(tx, "UNRESPONSIVE_HANDOFF_ALIVE") })
	t.Logf("failed importer PID=%d stopped; old pane accepts input; %s", pid, out)
}
