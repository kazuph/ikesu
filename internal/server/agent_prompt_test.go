package server

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/runtime"
	"github.com/kazuph/goterrace/internal/teststate"
)

func promptFixture(t *testing.T) (*Server, *runtime.Pane, string) {
	return promptFixtureAgent(t, "codex")
}

func promptFixtureAgent(t *testing.T, kind string) (*Server, *runtime.Pane, string) {
	t.Helper()
	teststate.Setenv(t, "SHELL", "/bin/sh")
	s := newTestServer(t, "prompt")
	done := make(chan struct{})
	go func() { s.app.Run(); close(done) }()
	t.Cleanup(func() { s.app.Stop(); <-done; s.sup.KillAll() })
	path := filepath.Join(t.TempDir(), kind)
	record := path + ".bytes"
	script := "import os,sys,tty\ntty.setraw(0)\nos.write(1,b'\\x1b[?2004hREADY')\nf=open(sys.argv[1],'wb',buffering=0)\nwhile True:\n b=os.read(0,1)\n if not b:break\n f.write(b)\n"
	if err := os.WriteFile(path, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	var started api.AgentStartedResult
	s.app.Do(func() {
		resp := s.agentStart("start", api.AgentStartParams{Argv: []string{"/usr/bin/python3", path, record}})
		if resp.Err != nil {
			t.Error(resp.Err)
			return
		}
		if err := json.Unmarshal(resp.Result, &started); err != nil {
			t.Error(err)
		}
	})
	pane := s.sup.Get(started.Agent.GlobalPaneNum)
	if pane == nil {
		t.Fatal("agent failed to spawn")
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("pane=%+v job=%+v", pane.Snapshot(), pane.ForegroundJob())
		}
	})
	waitPromptCondition(t, func() bool { return pane.BracketedPasteMode() && foregroundAgent(pane) == kind })
	starting := promptRequest(s, api.AgentPromptParams{Target: started.Agent.PaneID, Text: "starting must not write"})
	if starting.Err == nil || starting.Err.Code != "agent_not_ready" {
		t.Fatal(starting)
	}
	if b, _ := os.ReadFile(record); len(b) != 0 {
		t.Fatal("starting target received input", b)
	}

	s.app.Do(func() {
		s.paneReportAgent("report", api.PaneReportAgentParams{PaneID: started.Agent.PaneID, Source: "test", Agent: kind, State: "idle"})
	})
	waitPromptCondition(t, func() bool {
		var info api.AgentInfo
		s.app.Do(func() { info = s.agentInfoAt(pane.ID, 0, 1) })
		return info.InteractiveRdy
	})
	return s, pane, record
}

func promptReport(s *Server, status string) {
	s.app.Do(func() {
		s.paneReportAgent("report", api.PaneReportAgentParams{PaneID: "p2", Source: "test", Agent: "codex", State: status})
	})
}
func promptRequest(s *Server, p api.AgentPromptParams) api.Response {
	return s.agentPrompt(&api.Request{ID: "prompt", Method: api.MethodAgentPrompt, Params: api.MarshalParams(p)})
}
func waitPromptCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("PTY condition not reached")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestG9PromptRefusesBeforeInputAndWritesEnterBeforeSuccess(t *testing.T) {
	teststate.Lock(t)
	s, pane, record := promptFixture(t)
	for _, tc := range []struct{ status, text, code string }{{"idle", "", "empty_agent_prompt"}, {"idle", " \n", "empty_agent_prompt"}, {"blocked", "blocked", "agent_blocked"}, {"unknown", "unknown", "agent_not_ready"}} {
		promptReport(s, tc.status)
		resp := promptRequest(s, api.AgentPromptParams{Target: "p2", Text: tc.text})
		if resp.Err == nil || resp.Err.Code != tc.code {
			t.Fatalf("%+v: %+v", tc, resp)
		}
		if b, _ := os.ReadFile(record); len(b) != 0 {
			t.Fatal("refusal wrote input", b)
		}
	}
	promptReport(s, "idle")
	var want []byte
	for _, target := range []string{"p2", "p_2", "%2"} {
		text := "hello\n末尾\n"
		resp := promptRequest(s, api.AgentPromptParams{Target: target, Text: text})
		if resp.Err != nil {
			t.Fatal(resp.Err)
		}
		var result api.AgentPromptedResult
		if err := json.Unmarshal(resp.Result, &result); err != nil || result.Type != "agent_prompted" {
			t.Fatal(string(resp.Result), err)
		}
		want = append(want, []byte("\x1b[200~"+text+"\x1b[201~\r")...)
		waitPromptCondition(t, func() bool { b, _ := os.ReadFile(record); return len(b) >= len(want) })
		b, _ := os.ReadFile(record)
		if !bytes.Equal(b, want) {
			t.Fatalf("paste/Enter bytes=%q want=%q", b, want)
		}
	}
	// A report cannot turn a different foreground program into an agent.
	var other api.AgentStartedResult
	s.app.Do(func() {
		resp := s.agentStart("other", api.AgentStartParams{Argv: []string{"/bin/sleep", "60"}})
		json.Unmarshal(resp.Result, &other)
		s.paneReportAgent("report", api.PaneReportAgentParams{PaneID: other.Agent.PaneID, Source: "test", Agent: "codex", State: "idle"})
	})
	waitPromptCondition(t, func() bool {
		var info api.AgentInfo
		s.app.Do(func() { info = s.agentInfoAt(other.Agent.GlobalPaneNum, 0, 2) })
		return info.InteractiveRdy
	})
	resp := promptRequest(s, api.AgentPromptParams{Target: other.Agent.PaneID, Text: "must refuse"})
	if resp.Err == nil || resp.Err.Code != "agent_not_ready" {
		t.Fatal(resp)
	}
	s.sup.Kill(pane.ID)
}

func TestG9PromptWaitRequiresStartStalledTimeoutAndClose(t *testing.T) {
	teststate.Lock(t)
	s, _, _ := promptFixture(t)
	transition := make(chan struct{})
	go func() {
		defer close(transition)
		time.Sleep(600 * time.Millisecond)
		promptReport(s, "working")
		time.Sleep(150 * time.Millisecond)
		promptReport(s, "idle")
	}()
	resp := promptRequest(s, api.AgentPromptParams{Target: "p2", Text: "work", Wait: &api.AgentPromptWaitOptions{}})
	<-transition
	if resp.Err != nil {
		t.Fatal(resp.Err)
	}
	resp = promptRequest(s, api.AgentPromptParams{Target: "p2", Text: "stalled", Wait: &api.AgentPromptWaitOptions{}})
	if resp.Err == nil || resp.Err.Code != "agent_prompt_stalled" {
		t.Fatal(resp)
	}
	blocked := make(chan struct{})
	go func() { defer close(blocked); time.Sleep(600 * time.Millisecond); promptReport(s, "blocked") }()
	resp = promptRequest(s, api.AgentPromptParams{Target: "p2", Text: "blocked observation", Wait: &api.AgentPromptWaitOptions{Until: []api.AgentStatus{api.AgentBlocked, api.AgentDone}}})
	<-blocked
	if resp.Err != nil {
		t.Fatal(resp.Err)
	}
	var stopped api.AgentPromptedResult
	if err := json.Unmarshal(resp.Result, &stopped); err != nil || stopped.Agent.AgentStatus != api.AgentBlocked {
		t.Fatal(stopped, err)
	}
	promptReport(s, "idle")
	zero := uint64(0)
	resp = promptRequest(s, api.AgentPromptParams{Target: "p2", Text: "timeout", Wait: &api.AgentPromptWaitOptions{TimeoutMS: &zero}})
	if resp.Err == nil || resp.Err.Code != api.ErrTimeout {
		t.Fatal(resp)
	}
	closed := make(chan struct{})
	go func() { defer close(closed); time.Sleep(100 * time.Millisecond); s.app.Do(func() { s.sup.Kill(2) }) }()
	resp = promptRequest(s, api.AgentPromptParams{Target: "p2", Text: "close during submit"})
	<-closed
	if resp.Err == nil || resp.Err.Code != "agent_prompt_failed" {
		t.Fatal(resp)
	}
}

func TestG9CopilotPromptFocusBytesWithoutChangingUIFocus(t *testing.T) {
	teststate.Lock(t)
	s, _, record := promptFixtureAgent(t, "copilot")
	var before int
	s.app.Do(func() { before = s.app.State().ActiveWorkspace().ActiveLayout().Focused() })
	resp := promptRequest(s, api.AgentPromptParams{Target: "p2", Text: "focus\n"})
	if resp.Err != nil {
		t.Fatal(resp.Err)
	}
	want := []byte("\x1b[I\x1b[200~focus\n\x1b[201~\r")
	waitPromptCondition(t, func() bool { b, _ := os.ReadFile(record); return len(b) >= len(want) })
	if b, _ := os.ReadFile(record); !bytes.Equal(b, want) {
		t.Fatalf("copilot input=%q want=%q", b, want)
	}
	s.app.Do(func() {
		if s.app.State().ActiveWorkspace().ActiveLayout().Focused() != before {
			t.Error("prompt moved UI focus")
		}
	})
}
