package server

import (
	"io"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestEndpointExplicitFocusAndFocuslessMoveKeepViewerSelection(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	ready := dialClient(t, dir)
	ready.conn.Close()
	firstPane := firstPaneID(t, dir)
	reportAgent(t, dir, firstPane, "codex", "endpoint-focus-test")
	apiOK(t, dir, api.MethodPaneSplit, api.PaneSplitParams{Direction: "right"}, nil)
	ids := paneIDs(t, dir)
	if len(ids) != 2 {
		t.Fatal(ids)
	}
	remainingPane := ids[1]
	var destination, third api.TabCreatedResult
	apiOK(t, dir, api.MethodTabCreate, api.TabCreateParams{}, &destination)
	apiOK(t, dir, api.MethodTabCreate, api.TabCreateParams{}, &third)
	first, second := dialClient(t, dir), dialClient(t, dir)
	drained := make(chan struct{}, 2)
	for _, client := range []*testConn{first, second} {
		client.send(t, clientMsg{Type: "hello", Cols: 120, Rows: 40})
		client.readUntil(t, 5*time.Second, func(m clientMsg) bool { return m.Type == "output" })
		client.conn.SetReadDeadline(time.Time{})
		go func(c *testConn) { io.Copy(io.Discard, c.rd); drained <- struct{}{} }(client)
	}
	defer func() { first.conn.Close(); second.conn.Close(); <-drained; <-drained }()
	input := func(client *testConn, bytes string) {
		client.send(t, clientMsg{Type: "input", Data: encode64([]byte(bytes))})
	}
	marker := func(client *testConn, pane, text string) {
		input(client, "printf '"+text+"\\n'\n")
		waitRead(t, dir, pane, text)
	}
	input(second, "\x02n\x02n")
	marker(second, third.RootPane.PaneID, "THIRD_BEFORE_FOCUS")
	// The canonical server already focuses firstPane; the viewer does not.
	apiOK(t, dir, api.MethodAgentFocus, api.AgentTarget{Target: firstPane}, nil)
	marker(second, firstPane, "EXPLICIT_FOCUS_ALREADY_CANONICAL")
	input(second, "\x02n\x02n")
	marker(second, third.RootPane.PaneID, "THIRD_BEFORE_BACKGROUND")
	apiOK(t, dir, api.MethodTabCreate, api.TabCreateParams{}, nil)
	marker(second, third.RootPane.PaneID, "BACKGROUND_DID_NOT_SWITCH")
	split := "right"
	apiOK(t, dir, api.MethodPaneMove, api.PaneMoveParams{PaneID: firstPane, Destination: api.PaneMoveDestination{Type: "tab", TabID: destination.Tab.TabID, Split: &split}}, nil)
	marker(second, third.RootPane.PaneID, "FOCUSLESS_MOVE_DID_NOT_SWITCH")
	marker(first, remainingPane, "SOURCE_VIEW_REMAINS_AFTER_MOVE")
	apiOK(t, dir, api.MethodPaneMove, api.PaneMoveParams{PaneID: firstPane, Destination: api.PaneMoveDestination{Type: "new_workspace"}, Focus: true}, nil)
	marker(first, firstPane, "FOCUSED_MOVE_FIRST_VIEW")
	marker(second, firstPane, "FOCUSED_MOVE_SECOND_VIEW")
}
