package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/teststate"
	"github.com/kazuph/goterrace/internal/tui"
)

func review2FailedShell(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "gtxr2-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	shell := filepath.Join(root, "failed-path")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\ncase \"$*\" in *-lic*) exit 1;; esac\nexec /bin/sh \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "SHELL", shell)
	teststate.Setenv(t, "HOME", root)
	return root
}

func TestReview2RestoreLaunchedDespiteFailedLoginPATH(t *testing.T) {
	teststate.Lock(t)
	root := review2FailedShell(t)
	cfg := filepath.Join(root, "config.toml")
	if err := os.WriteFile(cfg, []byte("[session]\nresume_agents_on_restore = false\n[agent_restore.templates]\nclaude = \"/bin/echo {session_id}\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "GTX_CONFIG", cfg)
	teststate.Setenv(t, "GTX_STATE_DIR", filepath.Join(root, "state"))
	dir, done := startServerAt(t, "r2")
	defer func() { apiOK(t, dir, api.MethodServerStop, api.EmptyParams{}, nil); <-done }()
	// The real start API proves that the one cached login lookup failed.
	failed := apiReq(t, dir, api.MethodAgentStart, api.AgentStartParams{Argv: []string{"/bin/echo", "must-not-start"}})
	if failed.Err == nil || !strings.Contains(failed.Err.Message, "login shell PATH") {
		t.Fatal(failed)
	}
	reportAgent(t, dir, "p1", "claude", "sess-r2")
	reportAgent(t, dir, "p1", "", "")
	var res api.AgentRestoreResult
	apiOK(t, dir, api.MethodAgentRestore, api.AgentRestoreParams{}, &res)
	if len(res.Actions) != 1 || res.Actions[0].Status != "launched" || res.Actions[0].Reason != nil || res.Actions[0].Command == nil || *res.Actions[0].Command != "/bin/echo sess-r2" {
		t.Fatal(res)
	}
	var screen api.PaneReadResultEnvelope
	deadline := time.Now().Add(5 * time.Second)
	for {
		apiOK(t, dir, api.MethodPaneRead, api.PaneReadParams{PaneID: "p1", StripANSI: true}, &screen)
		if strings.Contains(screen.Read.Text, "/bin/echo sess-r2") && strings.Contains(screen.Read.Text, "\nsess-r2") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(screen.Read.Text)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if strings.Contains(screen.Read.Text, "PATH=") {
		t.Fatal("PATH was typed into pane", screen.Read.Text)
	}
	t.Logf("restore=%s command=%q screen=%q", res.Actions[0].Status, *res.Actions[0].Command, screen.Read.Text)
}

func TestReview2AutomaticRestoreDoesNotWaitForLoginPATH(t *testing.T) {
	teststate.Lock(t)
	review2FailedShell(t)
	s := newTestServer(t, "auto-r2")
	if err := s.sup.WaitLoginPath(); err == nil {
		t.Fatal("expected real login shell failure")
	}
	if err := s.app.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	s.cfg.AgentRestore.Templates = map[string]string{"claude": "/bin/echo {session_id}"}
	enabled := true
	delay := 0
	s.cfg.Session.ResumeAgentsOnRestore = &enabled
	s.cfg.AgentRestore.RestoreDelayMs = &delay
	s.app.RecordSession(1, tui.AgentRecord{Agent: "claude", Kind: "id", Value: "auto-r2", Source: "restore"})
	done := make(chan struct{})
	go func() { s.app.Run(); close(done) }()
	defer func() { s.app.Stop(); <-done; s.sup.KillAll() }()
	s.scheduleAutoResume()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var screen api.PaneReadResultEnvelope
		s.app.Do(func() {
			resp := s.paneRead("read", api.PaneReadParams{PaneID: "p1", StripANSI: true})
			if resp.Err != nil {
				t.Error(resp.Err)
				return
			}
			if err := json.Unmarshal(resp.Result, &screen); err != nil {
				t.Error(err)
			}
		})
		if strings.Contains(screen.Read.Text, "/bin/echo auto-r2") && strings.Contains(screen.Read.Text, "\nauto-r2") {
			if strings.Contains(screen.Read.Text, "PATH=") {
				t.Fatal(screen.Read.Text)
			}
			t.Logf("automatic restore screen=%q", screen.Read.Text)
			return
		}
		if time.Now().After(deadline) {
			t.Fatal(screen.Read.Text)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
