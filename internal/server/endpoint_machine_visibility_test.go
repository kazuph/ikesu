package server

import (
	"io"

	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/teststate"
)

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
