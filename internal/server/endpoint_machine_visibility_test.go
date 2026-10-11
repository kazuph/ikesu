package server

import (
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestHiddenMachineEndpointDoesNotReadTabsAndSelectsOriginalWorkspaceID(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	ready := dialClient(t, dir)
	ready.conn.Close()
	first := firstPaneID(t, dir)
	label := "remote-space"
	var created api.WorkspaceCreatedResult
	apiOK(t, dir, api.MethodWorkspaceCreate, api.WorkspaceCreateParams{Label: &label}, &created)
	for _, pane := range []string{first, created.RootPane.PaneID} {
		for _, state := range []string{"working", "idle"} {
			apiOK(t, dir, api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: pane, Source: "test", Agent: "codex", State: state}, nil)
		}
	}
	hidden := false
	c := dialClient(t, dir)
	defer c.conn.Close()
	c.send(t, clientMsg{Type: "hello", Cols: 120, Rows: 40, Visible: &hidden})
	c.readUntil(t, 5*time.Second, func(m clientMsg) bool { return m.Type == "output" })
	c.conn.SetReadDeadline(time.Time{})
	drained := make(chan struct{})
	go func() { io.Copy(io.Discard, c.rd); close(drained) }()
	defer func() { c.conn.Close(); <-drained }()
	status := func(pane, want string) {
		t.Helper()
		var result api.PaneInfoResult
		apiOK(t, dir, api.MethodPaneGet, api.PaneTarget{PaneID: pane}, &result)
		if result.Pane.AgentStatus != want {
			t.Fatalf("%s status=%s want %s", pane, result.Pane.AgentStatus, want)
		}
	}
	status(first, "done")
	status(created.RootPane.PaneID, "done")
	id, err := strconv.Atoi(strings.TrimPrefix(created.Workspace.WorkspaceID, "s"))
	if err != nil {
		t.Fatal(err)
	}
	c.send(t, clientMsg{Type: "workspace_select", Workspace: id})
	status(created.RootPane.PaneID, "done")
	visible := true
	c.send(t, clientMsg{Type: "visibility", Visible: &visible})
	c.send(t, clientMsg{Type: "input", Data: encode64([]byte("printf 'REMOTE_%s\\n' SELECTED\n"))})
	waitRead(t, dir, created.RootPane.PaneID, "REMOTE_SELECTED")
	status(first, "done")
	status(created.RootPane.PaneID, "idle")
	var workspaces api.WorkspaceListResult
	apiOK(t, dir, api.MethodWorkspaceList, nil, &workspaces)
	if len(workspaces.Workspaces) != 2 || !workspaces.Workspaces[0].Focused || workspaces.Workspaces[1].Focused {
		t.Fatal("remote viewer navigation changed the canonical session workspace", workspaces)
	}
}

func TestLegacyRealServerRejectsHiddenMachineBeforeMarkingSeenButKeepsLocal(t *testing.T) {
	teststate.Lock(t)
	legacy := endpoint.Peer{Protocol: endpoint.ProtocolVersion, Generation: 1, MinGeneration: 1,
		Methods: []string{"input", "resize", "rendered", "preferences_error", "config_result"}}
	dir, done := startTestEndpoint(t, legacy)
	defer stopServer(t, dir, done)
	ready := dialClient(t, dir)
	ready.conn.Close()
	pid := firstPaneID(t, dir)
	for _, state := range []string{"working", "idle"} {
		apiOK(t, dir, api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: pid, Source: "test", Agent: "codex", State: state}, nil)
	}
	hidden := false
	remote := dialClient(t, dir)
	defer remote.conn.Close()
	remote.send(t, clientMsg{Peer: endpoint.MachinePeer(), Type: "hello", Cols: 120, Rows: 40, Visible: &hidden})
	remote.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	rejection, err := readClientMsg(remote.rd)
	if err != nil || rejection.Type != "error" || !strings.HasPrefix(rejection.Reason, "endpoint_generation_newer:") {
		t.Fatal("hidden connection was not rejected at the handshake", rejection, err)
	}
	if message, err := readClientMsg(remote.rd); err == nil {
		t.Fatal("rejected hidden viewer received another message", message)
	}
	var pane api.PaneInfoResult
	apiOK(t, dir, api.MethodPaneGet, api.PaneTarget{PaneID: pid}, &pane)
	if pane.Pane.AgentStatus != "done" {
		t.Fatal("rejected hidden connection marked the pane seen", pane)
	}
	local := dialClient(t, dir)
	defer local.conn.Close()
	local.send(t, clientMsg{Peer: endpoint.Current(), Type: "hello", Cols: 120, Rows: 40})
	local.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	welcome, err := readClientMsg(local.rd)
	if err != nil || welcome.Type != "welcome" || welcome.Available != 1 {
		t.Fatal("Local did not negotiate with the generation-one real server", welcome, err)
	}
	local.conn.SetReadDeadline(time.Time{})
	drained := make(chan struct{})
	go func() { io.Copy(io.Discard, local.rd); close(drained) }()
	defer func() { local.conn.Close(); <-drained }()
	local.send(t, clientMsg{Type: "input", Data: encode64([]byte("printf 'LOCAL_%s\\n' LEGACY_OK\n"))})
	waitRead(t, dir, pid, "LOCAL_LEGACY_OK")
}
