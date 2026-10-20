package server

import (
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestEndpointBothViewersRetainInputDuringSharedTabRedraw(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	ready := dialClient(t, dir)
	ready.conn.Close()
	pid := firstPaneID(t, dir)
	first, second := dialClient(t, dir), dialClient(t, dir)
	drained := make(chan struct{}, 2)
	for _, c := range []*testConn{first, second} {
		c.send(t, clientMsg{Type: "hello", Cols: 160, Rows: 45})
		c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			message, err := readClientMsg(c.rd)
			if err != nil {
				t.Fatalf("first snapshot: %v", err)
			}
			if message.Type == "snapshot" {
				break
			}
		}
		c.conn.SetReadDeadline(time.Time{})
		go func(client *testConn) {
			io.Copy(io.Discard, client.rd)
			drained <- struct{}{}
		}(c)
	}
	defer func() { first.conn.Close(); second.conn.Close(); <-drained; <-drained }()
	// Neither viewer acknowledges subsequent redraws. Resize and shell input
	// alternate between them after each has received its first real snapshot.
	for index := 0; index < 10; index++ {
		for _, row := range []struct {
			client *testConn
			name   string
			cols   int
		}{{first, "FIRST", 80}, {second, "SECOND", 160}} {
			row.client.send(t, clientMsg{Type: "resize", Cols: row.cols, Rows: 45})
			command := fmt.Sprintf("printf '%s_%%s\\n' %02d\n", row.name, index)
			row.client.send(t, clientMsg{Type: "input", Data: encode64([]byte(command))})
		}
	}
	for index := 0; index < 10; index++ {
		for _, name := range []string{"FIRST", "SECOND"} {
			waitRead(t, dir, pid, fmt.Sprintf("%s_%02d", name, index))
		}
	}
}

func TestEndpointOnlyFocusedVisibleTabsBecomeRead(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	ready := dialClient(t, dir)
	ready.conn.Close()
	firstPane := firstPaneID(t, dir)
	var secondTab, hiddenTab api.TabCreatedResult
	apiOK(t, dir, api.MethodTabCreate, api.TabCreateParams{}, &secondTab)
	apiOK(t, dir, api.MethodTabCreate, api.TabCreateParams{}, &hiddenTab)
	first, second := dialClient(t, dir), dialClient(t, dir)
	drained := make(chan struct{}, 2)
	for _, c := range []*testConn{first, second} {
		c.send(t, clientMsg{Type: "hello", Cols: 120, Rows: 40})
		c.readUntil(t, 5*time.Second, func(m clientMsg) bool { return m.Type == "output" })
		c.conn.SetReadDeadline(time.Time{})
		go func(client *testConn) { io.Copy(io.Discard, client.rd); drained <- struct{}{} }(c)
	}
	defer func() { first.conn.Close(); second.conn.Close(); <-drained; <-drained }()
	second.send(t, clientMsg{Type: "input", Data: encode64([]byte("\x02nprintf 'SECOND_%s\\n' VISIBLE\n"))})
	waitRead(t, dir, secondTab.RootPane.PaneID, "SECOND_VISIBLE")
	first.send(t, clientMsg{Type: "input", Data: encode64([]byte("\x1b[O"))})
	// A following shell marker proves the blur was handled before completion.
	first.send(t, clientMsg{Type: "input", Data: encode64([]byte("printf 'FIRST_%s\\n' BLURRED\n"))})
	waitRead(t, dir, firstPane, "FIRST_BLURRED")
	panes := []string{firstPane, secondTab.RootPane.PaneID, hiddenTab.RootPane.PaneID}
	for _, pane := range panes {
		for _, state := range []string{"working", "idle"} {
			apiOK(t, dir, api.MethodPaneReportAgent, api.PaneReportAgentParams{PaneID: pane, Source: "test", Agent: "codex", State: state}, nil)
		}
	}
	assertStatus := func(pane, want string) {
		t.Helper()
		var result api.PaneInfoResult
		apiOK(t, dir, api.MethodPaneGet, api.PaneTarget{PaneID: pane}, &result)
		if result.Pane.AgentStatus != want {
			t.Fatalf("%s status=%s, want %s", pane, result.Pane.AgentStatus, want)
		}
	}
	assertStatus(firstPane, "done")
	assertStatus(secondTab.RootPane.PaneID, "idle")
	assertStatus(hiddenTab.RootPane.PaneID, "done")
	first.send(t, clientMsg{Type: "input", Data: encode64([]byte("\x1b[Iprintf 'FIRST_%s\\n' REFOCUSED\n"))})
	waitRead(t, dir, firstPane, "FIRST_REFOCUSED")
	assertStatus(firstPane, "idle")
	assertStatus(secondTab.RootPane.PaneID, "idle")
	assertStatus(hiddenTab.RootPane.PaneID, "done")
	var tabs api.TabListResult
	apiOK(t, dir, api.MethodTabList, api.TabListParams{}, &tabs)
	if !tabs.Tabs[0].Focused || tabs.Tabs[1].Focused || tabs.Tabs[2].Focused {
		t.Fatal("viewer navigation changed the session focus", tabs)
	}
}
