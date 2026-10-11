package server

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestEndpointRestoreProgressesDuringUnacknowledgedRedraw(t *testing.T) {
	teststate.Lock(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("onboarding=false\n[session]\nresume_agents_on_restore=false\n[agent_restore.commands]\nclaude='/bin/echo {session_id}'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, config.EnvVar, path)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	ready := dialClient(t, dir)
	ready.conn.Close()
	pid := firstPaneID(t, dir)
	reportAgent(t, dir, pid, "claude", "endpoint-restore")
	reportAgent(t, dir, pid, "", "")
	first, second := dialClient(t, dir), dialClient(t, dir)
	drained := make(chan struct{}, 2)
	for _, client := range []*testConn{first, second} {
		client.send(t, clientMsg{Type: "hello", Cols: 160, Rows: 45})
		client.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			message, err := readClientMsg(client.rd)
			if err != nil {
				t.Fatal(err)
			}
			if message.Type == "snapshot" {
				break
			}
		}
		client.conn.SetReadDeadline(time.Time{})
		go func(c *testConn) { io.Copy(io.Discard, c.rd); drained <- struct{}{} }(client)
	}
	defer func() { first.conn.Close(); second.conn.Close(); <-drained; <-drained }()
	for index := 0; index < 10; index++ {
		first.send(t, clientMsg{Type: "resize", Cols: 80, Rows: 24})
		second.send(t, clientMsg{Type: "resize", Cols: 160, Rows: 45})
	}
	var result api.AgentRestoreResult
	apiOK(t, dir, api.MethodAgentRestore, api.AgentRestoreParams{}, &result)
	if len(result.Actions) != 1 || result.Actions[0].Status != "launched" || result.Actions[0].Command == nil || *result.Actions[0].Command != "command sh -lc '/bin/echo endpoint-restore'" {
		t.Fatal(result)
	}
	// The leading newline distinguishes the real command output from the
	// echoed command line. No actual agent executable is launched.
	deadline := time.Now().Add(5 * time.Second)
	for {
		var screen api.PaneReadResultEnvelope
		apiOK(t, dir, api.MethodPaneRead, api.PaneReadParams{PaneID: pid, StripANSI: true}, &screen)
		if strings.Contains(screen.Read.Text, "\nendpoint-restore") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("restore output did not reach the real PTY", screen.Read.Text)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
