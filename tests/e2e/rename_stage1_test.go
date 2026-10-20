//go:build rename_stage1

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

// The pre-rename binary must be built from unchanged main 2e01183.
func TestRenameStageOneMixedHandoffAndRollback(t *testing.T) {
	old := os.Getenv("RENAME_OLD_BIN")
	if old == "" || filepath.Base(old) != "gtx" {
		t.Fatal("RENAME_OLD_BIN must point to main 2e01183 binary named gtx")
	}
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, []byte("onboarding=false\n[ui]\nsidebar_width=23\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(t.TempDir(), "goterrace")
	if err := os.MkdirAll(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(t.TempDir(), "old-server")
	if err := os.WriteFile(wrapper, []byte(fmt.Sprintf("#!/bin/sh\ncd %q\nexec %q \"$@\"\n", cwd, old)), 0700); err != nil {
		t.Fatal(err)
	}
	s := startVersionMode(t, map[string]string{"GTX_CONFIG": cfg}, false, binPath, wrapper)
	s.waitFor("new client attached to old server", 15*time.Second, func(text string) bool { return strings.Contains(text, "❯") })
	pidfile := filepath.Join(t.TempDir(), "identity")
	s.send(fmt.Sprintf("printf '%%s %%s\\n' $$ \"$PWD\" > %q; %q pane current >> %q; printf 'OLD_COMMAND_%%s\\n' READY\r", pidfile, old, pidfile))
	s.waitFor("old pane old CLI", 10*time.Second, func(text string) bool { return strings.Contains(text, "OLD_COMMAND_READY") })
	before, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(before), cwd) || !strings.Contains(string(before), `"pane_id":"p1"`) {
		t.Fatal(string(before))
	}
	var panes api.PaneListResult
	s.rpc(api.MethodPaneList, api.PaneListParams{}, &panes)
	paneID := "p1"
	var split api.PaneInfoResult
	// Wire fields remain the same, regardless of the client's new name.
	s.rpc(api.MethodPaneSplit, api.PaneSplitParams{TargetPaneID: &paneID, Direction: "right", Focus: false}, &split)
	var prepared api.PaneListResult
	s.rpc(api.MethodPaneList, api.PaneListParams{}, &prepared)
	if len(prepared.Panes) != len(panes.Panes)+1 {
		t.Fatal(prepared)
	}
	var question api.DecisionResult
	s.rpc(api.MethodDecisionCreate, api.DecisionCreateParams{Kind: "ask", Title: "RenamePending", Options: []api.DecisionOption{{ID: "yes", Label: "Yes", Role: "approve"}}}, &question)
	for _, exe := range []string{binPath, old} {
		if out, err := s.cli("server", "live-handoff", "--import-exe", exe); err != nil {
			t.Fatalf("handoff to %s: %v %s", exe, err, out)
		}
		var current api.PaneListResult
		s.rpc(api.MethodPaneList, api.PaneListParams{}, &current)
		beforeIDs, afterIDs := []string{}, []string{}
		for _, p := range prepared.Panes {
			beforeIDs = append(beforeIDs, p.PaneID+":"+p.TerminalID+":"+p.WorkspaceID+":"+p.TabID)
		}
		for _, p := range current.Panes {
			afterIDs = append(afterIDs, p.PaneID+":"+p.TerminalID+":"+p.WorkspaceID+":"+p.TabID)
		}
		if !reflect.DeepEqual(beforeIDs, afterIDs) {
			t.Fatalf("IDs changed %v / %v", beforeIDs, afterIDs)
		}
		var retained api.DecisionResult
		s.rpc(api.MethodDecisionGet, api.DecisionTargetParams{DecisionID: question.Decision.DecisionID}, &retained)
		if retained.Decision.Status != "pending" {
			t.Fatal(retained)
		}
		marker := "AFTER_" + filepath.Base(exe) + "_HANDOFF"
		s.send(fmt.Sprintf("printf '%%s %%s\\n' $$ \"$PWD\" >> %q; %q pane current >> %q; printf '%%s\\n' %q\r", pidfile, binPath, pidfile, marker))
		s.waitFor("old pane new CLI after handoff", 10*time.Second, func(text string) bool { return strings.Contains(text, marker) })
		data, err := os.ReadFile(pidfile)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if lines[0] != lines[len(lines)-2] {
			t.Fatalf("shell PID/cwd changed: %s", data)
		}
		var decoded struct {
			Result api.PaneCurrentResult `json:"result"`
		}
		if err := json.Unmarshal([]byte(lines[len(lines)-1]), &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Result.Pane.PaneID != "p1" {
			t.Fatal(decoded)
		}
		t.Logf("handoff to=%s retained IDs=%v shell=%s config=%s", exe, afterIDs, lines[0], cfg)
	}
	if out, err := s.cli("decision", "answer", question.Decision.DecisionID, "yes"); err != nil {
		t.Fatal(out, err)
	}
	var answered api.DecisionResult
	s.rpc(api.MethodDecisionGet, api.DecisionTargetParams{DecisionID: question.Decision.DecisionID}, &answered)
	if answered.Decision.Status != "answered" {
		t.Fatal(answered)
	}
}
