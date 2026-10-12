package e2e

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/kazuph/goterrace/internal/api"
	"golang.org/x/sys/unix"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

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
	s := start(t)
	s.waitFor("shell", 15*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	sessionID := "handoff-saved-conversation"
	seq := uint64(8)
	s.rpc(api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: "p1", Source: "handoff", Agent: "codex", State: "working", Seq: &seq, SessionID: &sessionID}, nil)
	marker := filepath.Join(s.stateDir, "guard", "agent-panes", "p_1.json")
	before, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	link := func(id string) {
		root := t.TempDir()
		manifest := fmt.Sprintf("id=%q\nname=%q\nversion=\"0.1.0\"\nmin_gtx_version=\"0.1.0\"\nplatforms=[\"macos\",\"linux\"]\n[[panes]]\nid=\"board\"\ntitle=\"Board\"\ncommand=[\"/bin/cat\"]\n", id, id)
		if err := os.WriteFile(filepath.Join(root, "gtx-plugin.toml"), []byte(manifest), 0600); err != nil {
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

func TestLiveHandoffUnresponsiveImporterRetainsPane(t *testing.T) {
	s := start(t)
	s.waitFor("shell", 15*time.Second, func(tx string) bool { return strings.Contains(tx, "❯") })
	root := t.TempDir()
	importer := filepath.Join(root, "importer")
	pidfile := filepath.Join(root, "pid")
	if err := os.WriteFile(importer, []byte(fmt.Sprintf("#!/bin/sh\nprintf '%%s' $$ > %q\nexec /bin/sleep 30\n", pidfile)), 0700); err != nil {
		t.Fatal(err)
	}
	out, err := s.cli("server", "live-handoff", "--import-exe", importer)
	if err == nil || !strings.Contains(out, "handoff_failed") || !strings.Contains(out, "did not respond") {
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
