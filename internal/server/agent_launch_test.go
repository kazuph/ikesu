package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestG9AgentStartupWrapperSettlesBlockedAndDeadline(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, "SHELL", "/bin/sh")
	s := newTestServer(t, "startup")
	done := make(chan struct{})
	go func() { s.app.Run(); close(done) }()
	defer func() { s.app.Stop(); <-done; s.sup.KillAll() }()
	argv := []string{"/bin/sh", "-c", "exec /bin/sleep 60"}
	var started api.AgentStartedResult
	s.app.Do(func() {
		resp := s.agentStart("start", api.AgentStartParams{Argv: argv})
		if resp.Err != nil {
			t.Error(resp.Err)
			return
		}
		json.Unmarshal(resp.Result, &started)
	})
	if started.Type != "agent_started" || !started.Agent.LaunchPending || started.Agent.InteractiveRdy || len(started.Argv) != len(argv) {
		t.Fatal(started)
	}
	pid := started.Agent.GlobalPaneNum
	get := func() api.AgentInfo {
		var info api.AgentInfo
		s.app.Do(func() { info = s.agentInfoAt(pid, 0, 1) })
		return info
	}
	report := func(status string) {
		s.app.Do(func() {
			s.paneReportAgent("report", api.PaneReportAgentParams{PaneID: started.Agent.PaneID, Source: "test", Agent: "codex", State: status})
		})
	}
	report("blocked")
	if info := get(); info.LaunchPending || info.InteractiveRdy || info.AgentStatus != api.AgentBlocked {
		t.Fatal(info)
	}
	report("idle")
	before := time.Now()
	if info := get(); !info.LaunchPending || info.InteractiveRdy {
		t.Fatal(info)
	}
	resp := promptRequest(s, api.AgentPromptParams{Target: started.Agent.PaneID, Text: "starting"})
	if resp.Err == nil || resp.Err.Code != "agent_not_ready" {
		t.Fatal(resp)
	}
	time.Sleep(agentLaunchSettle)
	if info := get(); info.LaunchPending || !info.InteractiveRdy || time.Since(before) < agentLaunchSettle {
		t.Fatal(info)
	}
	// An unready wrapper has the full SPEC 30-second deadline, not a shorter test path.
	var unready api.AgentStartedResult
	s.app.Do(func() {
		resp := s.agentStart("unready", api.AgentStartParams{Argv: argv})
		json.Unmarshal(resp.Result, &unready)
		s.paneReportAgent("report", api.PaneReportAgentParams{PaneID: unready.Agent.PaneID, Source: "test", Agent: "codex", State: "unknown"})
	})
	time.Sleep(agentLaunchDeadline)
	s.app.Do(func() {
		info := s.agentInfoAt(unready.Agent.GlobalPaneNum, 0, 2)
		if info.LaunchPending || info.InteractiveRdy {
			t.Error(info)
		}
		info = s.agentInfoAt(unready.Agent.GlobalPaneNum, 0, 2)
		if info.LaunchPending || info.InteractiveRdy {
			t.Error("expired launch became ready", info)
		}
	})
}
