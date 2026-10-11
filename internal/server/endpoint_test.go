package server

import (
	"bufio"
	"fmt"
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/model"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestEndpointSnapshotCarriesStateAndPaneCells(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	client := dialClient(t, dir)
	defer client.conn.Close()
	client.send(t, clientMsg{Type: "hello", Cols: 160, Rows: 45})
	client.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	rd := bufio.NewReader(client.conn)
	var decoder endpoint.SnapshotDecoder
	for {
		message, err := readClientMsg(rd)
		if err != nil {
			t.Fatal(err)
		}
		if message.Type != "snapshot" {
			continue
		}
		data, err := decode64(message.Data)
		if err != nil {
			t.Fatal(err)
		}
		in, err := decoder.Decode(data)
		if err != nil {
			t.Fatal(err)
		}
		if in.State.Cols != 160 || in.State.Rows != 45 || in.State.ActiveWorkspace() == nil {
			t.Fatal("missing viewer state", in.State)
		}
		pane := in.Panes[in.State.FocusedPane()]
		if pane == nil || len(pane.Grid) == 0 || len(pane.Grid[0]) == 0 {
			t.Fatal("missing pane content cells")
		}
		return
	}
}

func TestEndpointFocusReturnRetainsImmediateKeys(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	client := dialClient(t, dir)
	defer client.conn.Close()
	client.send(t, clientMsg{Type: "hello", Cols: 80, Rows: 24})
	client.readUntil(t, 5*time.Second, func(m clientMsg) bool { return m.Type == "output" })
	pid := firstPaneID(t, dir)
	client.send(t, clientMsg{Type: "input", Data: encode64([]byte("\x1b[O\x1b[Iprintf 'FOCUS_%s\\n' KEPT\n"))})
	client.readUntil(t, 5*time.Second, func(m clientMsg) bool {
		data, _ := decode64(m.Data)
		return strings.Contains(string(data), "FOCUS_KEPT")
	})
	waitRead(t, dir, pid, "FOCUS_KEPT")
}

func TestEndpointGenerationRangeAndUnsupportedAction(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	client := dialClient(t, dir)
	defer client.conn.Close()
	peer := endpoint.Current()
	peer.Generation = 2
	client.send(t, clientMsg{Type: "hello", Peer: peer, Cols: 80, Rows: 24})
	messages := client.readUntil(t, 5*time.Second, func(m clientMsg) bool { return m.Type == "welcome" })
	if len(messages) != 1 || messages[0].Type != "welcome" || messages[0].Available != 1 {
		t.Fatal(messages)
	}
	client.send(t, clientMsg{Type: "machine.route"})
	messages = client.readUntil(t, 5*time.Second, func(m clientMsg) bool { return m.Type == "error" })
	if len(messages) == 0 || !strings.HasPrefix(messages[len(messages)-1].Reason, "endpoint_action_unsupported:") {
		t.Fatal(messages)
	}
	client.send(t, clientMsg{Type: "input", Data: encode64([]byte("echo rangestillrunning\n"))})
	messages = client.readUntil(t, 5*time.Second, func(m clientMsg) bool {
		b, _ := decode64(m.Data)
		return strings.Contains(string(b), "rangestillrunning")
	})
	if !strings.Contains(outputs(messages), "rangestillrunning") {
		t.Fatal("action rejection closed connection")
	}
}

func TestEndpointRejectsGenerationBeforeAttach(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	client := dialClient(t, dir)
	defer client.conn.Close()
	client.send(t, clientMsg{Type: "hello", Peer: endpoint.Peer{Protocol: 1}, Cols: 80, Rows: 24})
	messages := client.readUntil(t, 5*time.Second, func(m clientMsg) bool { return m.Type == "error" })
	if len(messages) != 1 || !strings.Contains(messages[0].Reason, "endpoint_generation_unsupported: upgrade") {
		t.Fatal(messages)
	}
}

func TestEndpointIndependentTabsAndLastInputSize(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	ready := dialClient(t, dir)
	ready.conn.Close()
	firstPane := firstPaneID(t, dir)
	var tab api.TabCreatedResult
	apiOK(t, dir, api.MethodTabCreate, api.TabCreateParams{}, &tab)
	first := dialClient(t, dir)
	defer first.conn.Close()
	first.send(t, clientMsg{Type: "hello", Cols: 80, Rows: 24})
	first.readUntil(t, 5*time.Second, func(m clientMsg) bool { return m.Type == "output" })
	first.conn.SetReadDeadline(time.Time{})
	firstDrained := make(chan struct{})
	go func() { io.Copy(io.Discard, first.rd); close(firstDrained) }()
	second := dialClient(t, dir)
	defer second.conn.Close()
	second.send(t, clientMsg{Type: "hello", Cols: 160, Rows: 45})
	second.readUntil(t, 5*time.Second, func(m clientMsg) bool { return m.Type == "output" })
	first.conn.SetReadDeadline(time.Time{})
	second.conn.SetReadDeadline(time.Time{})
	secondDrained := make(chan struct{})
	go func() { io.Copy(io.Discard, second.rd); close(secondDrained) }()
	defer func() { first.conn.Close(); second.conn.Close(); <-firstDrained; <-secondDrained }()
	second.send(t, clientMsg{Type: "input", Data: encode64([]byte{2, 'n'})})
	second.send(t, clientMsg{Type: "input", Data: encode64([]byte("printf 'SECOND_TAB_ONLY\\n'\n"))})
	waitRead(t, dir, tab.RootPane.PaneID, "SECOND_TAB_ONLY")
	first.send(t, clientMsg{Type: "input", Data: encode64([]byte("printf 'FIRST_TAB_ONLY\\n'\n"))})
	waitRead(t, dir, firstPane, "FIRST_TAB_ONLY")
	var tabs api.TabListResult
	apiOK(t, dir, api.MethodTabList, api.TabListParams{}, &tabs)
	if !tabs.Tabs[0].Focused || tabs.Tabs[1].Focused {
		t.Fatal("viewer navigation changed session active tab", tabs)
	}
	second.send(t, clientMsg{Type: "input", Data: encode64([]byte{2, 'p'})})
	second.send(t, clientMsg{Type: "input", Data: encode64([]byte("printf 'RETURN_SHARED_TAB\\n'\n"))})
	waitRead(t, dir, firstPane, "RETURN_SHARED_TAB")
	expected := func(cols, rows int) string {
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		state := model.New("", cols, rows)
		state.SidebarMin, state.SidebarMax, state.SidebarDefault, err = cfg.SidebarBounds()
		if err != nil {
			t.Fatal(err)
		}
		state.SidebarSize = state.SidebarDefault
		width, height := model.PaneTermSize(state.PanesArea(), true)
		return fmt.Sprintf("%d %d", height, width)
	}
	for _, test := range []struct {
		client     *testConn
		cols, rows int
		marker     string
	}{{first, 80, 24, "FIRSTSIZE"}, {second, 160, 45, "SECONDSIZE"}, {first, 80, 24, "RESTOREDSIZE"}} {
		test.client.send(t, clientMsg{Type: "input", Data: encode64([]byte("printf '" + test.marker + " '; stty size\n"))})
		waitRead(t, dir, firstPane, test.marker+" "+expected(test.cols, test.rows))
	}
}
