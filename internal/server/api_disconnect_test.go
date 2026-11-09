package server

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestDisconnectedAPIRequestDoesNotRunAfterAppResumes(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "disconnected-api")
	dir, err := os.MkdirTemp("/tmp", "ikesu-api-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	ln, err := net.Listen("unix", filepath.Join(dir, "api.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	appDone := make(chan struct{})
	go func() { s.app.Run(); close(appDone) }()
	defer func() { s.app.Stop(); <-appDone }()
	blocked := make(chan struct{})
	release := make(chan struct{})
	go s.app.Do(func() { close(blocked); <-release })
	<-blocked
	label := "must-not-appear"
	requests := []api.Request{
		{ID: "test", Method: api.MethodPaneRename, Params: api.MarshalParams(api.PaneRenameParams{PaneID: "p1", Label: &label})},
		{ID: "read", Method: api.MethodPaneList},
		{ID: "prompt", Method: api.MethodAgentPrompt, Params: api.MarshalParams(api.AgentPromptParams{Target: "p1", Text: "do not run"})},
		{ID: "output", Method: api.MethodPaneWaitForOutput, Params: api.MarshalParams(api.PaneWaitForOutputParams{PaneID: "p1", Match: api.OutputMatch{Type: "substring", Value: "missing"}})},
	}
	served := make(chan struct{}, len(requests))
	go func() {
		for range requests {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			s.apiWG.Add(1)
			go func() { s.serveAPI(conn); served <- struct{}{} }()
		}
	}()
	var connections []net.Conn
	for _, req := range requests {
		conn, err := net.Dial("unix", ln.Addr().String())
		if err != nil {
			close(release)
			t.Fatal(err)
		}
		connections = append(connections, conn)
		if err := api.WriteRequest(conn, req); err != nil {
			close(release)
			t.Fatal(err)
		}
	}
	<-time.After(100 * time.Millisecond)
	for _, conn := range connections {
		conn.Close()
	}
	var retained bool
	completed := 0
	deadline := time.After(300 * time.Millisecond)
	for completed < len(requests) {
		select {
		case <-served:
			completed++
		case <-deadline:
			retained = true
			goto unblocked
		}
	}
unblocked:
	close(release)
	for completed < len(requests) {
		<-served
		completed++
	}
	var got string
	s.app.Do(func() { got = s.app.State().PaneLabels[1] })
	if retained || got == label {
		t.Fatalf("disconnected request retained=%v label=%q", retained, got)
	}
}

func TestAPIHalfClosedWriteStillReceivesResponse(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "api-half-close")
	dir, err := os.MkdirTemp("/tmp", "ikesu-api-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	ln, err := net.Listen("unix", filepath.Join(dir, "api.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	appDone := make(chan struct{})
	go func() { s.app.Run(); close(appDone) }()
	defer func() { s.app.Stop(); <-appDone }()
	served := make(chan struct{})
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			s.apiWG.Add(1)
			s.serveAPI(conn)
		}
		close(served)
	}()
	conn, err := net.Dial("unix", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := api.WriteRequest(conn, api.Request{ID: "test", Method: api.MethodPaneList}); err != nil {
		t.Fatal(err)
	}
	if err := conn.(*net.UnixConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	resp, err := api.ReadResponse(bufio.NewReader(conn))
	if err != nil || resp.Err != nil {
		t.Fatalf("half-close response: %+v %v", resp, err)
	}
	<-served
}
