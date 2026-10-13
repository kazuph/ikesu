package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/teststate"
)

func directAgentClient(t *testing.T, dir, target string, takeover bool) *testConn {
	t.Helper()
	c := dialClient(t, dir)
	data, _ := json.Marshal(agentAttachTarget{Target: target, Takeover: takeover})
	c.send(t, clientMsg{Type: "agent_attach", Data: string(data), Cols: 80, Rows: 24})
	return c
}

func directMessage(t *testing.T, c *testConn) clientMsg {
	t.Helper()
	_ = c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	m, err := readClientMsg(c.rd)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestAgentAttachOwnership(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	waitAPISocket(t, dir)
	defer func() { apiOK(t, dir, api.MethodServerStop, api.EmptyParams{}, nil); <-done }()
	var created api.WorkspaceCreatedResult
	apiOK(t, dir, api.MethodWorkspaceCreate, api.WorkspaceCreateParams{Focus: true}, &created)
	apiOK(t, dir, api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: created.RootPane.PaneID, Source: "test", Agent: "codex", State: "idle"}, nil)
	name := "direct"
	apiOK(t, dir, api.MethodAgentRename, api.AgentRenameParams{Target: created.RootPane.PaneID, Name: &name}, nil)
	first := directAgentClient(t, dir, name, false)
	defer first.conn.Close()
	if m := directMessage(t, first); m.Type != "agent_attached" {
		t.Fatal(m)
	}
	first.send(t, clientMsg{Type: "input", Data: encode64([]byte("printf DIRECT_INPUT_OK\\n\n"))})
	waitRead(t, dir, created.RootPane.PaneID, "DIRECT_INPUT_OK")
	second := directAgentClient(t, dir, name, false)
	defer second.conn.Close()
	if m := directMessage(t, second); m.Type != "error" || !strings.Contains(m.Reason, "already has an attached client") {
		t.Fatal(m)
	}
	takeover := directAgentClient(t, dir, name, true)
	defer takeover.conn.Close()
	if m := directMessage(t, takeover); m.Type != "agent_attached" {
		t.Fatal(m)
	}
	for {
		m := directMessage(t, first)
		if m.Type == "error" {
			if !strings.Contains(m.Reason, "taken over") {
				t.Fatal(m)
			}
			break
		}
	}
	takeover.send(t, clientMsg{Type: "resize", Cols: 100, Rows: 30})
	takeover.send(t, clientMsg{Type: "input", Data: encode64([]byte("stty size\n"))})
	waitRead(t, dir, created.RootPane.PaneID, "30 100")
	takeover.send(t, clientMsg{Type: "release"})
	for {
		_, err := readClientMsg(takeover.rd)
		if err != nil {
			break
		}
	}
	next := directAgentClient(t, dir, name, false)
	defer next.conn.Close()
	if m := directMessage(t, next); m.Type != "agent_attached" {
		t.Fatal(m)
	}
	missing := directAgentClient(t, dir, "missing", false)
	defer missing.conn.Close()
	if m := directMessage(t, missing); m.Type != "error" {
		t.Fatal(m)
	}
}
