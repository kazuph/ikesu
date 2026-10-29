package server

import (
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/teststate"
	"testing"
	"time"
)

func TestStructuralChangesKeepIndependentViewerIDs(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	ready := dialClient(t, dir)
	ready.conn.Close()
	var second, third api.WorkspaceCreatedResult
	apiOK(t, dir, api.MethodWorkspaceCreate, api.WorkspaceCreateParams{}, &second)
	apiOK(t, dir, api.MethodWorkspaceCreate, api.WorkspaceCreateParams{Focus: true}, &third)
	a, b := dialClient(t, dir), dialClient(t, dir)
	defer a.conn.Close()
	defer b.conn.Close()
	a.send(t, clientMsg{Type: "hello", Cols: 120, Rows: 40, Selection: &endpoint.ViewSelection{Workspace: 2, Tabs: map[int]int{2: 2}, Pane: 2}})
	b.send(t, clientMsg{Type: "hello", Cols: 120, Rows: 40, Selection: &endpoint.ViewSelection{Workspace: 3, Tabs: map[int]int{3: 3}, Pane: 3}})
	check := func(c *testConn, label string, ws, tab, pane int, ready func() bool) {
		t.Helper()
		c.readUntil(t, 5*time.Second, func(m clientMsg) bool { return c.lastInput != nil && ready() })
		selection := endpoint.CaptureSelection(c.lastInput.State)
		t.Logf("%s workspace=%d tab=%d pane=%d", label, selection.Workspace, selection.Tabs[selection.Workspace], selection.Pane)
		if selection.Workspace != ws || selection.Tabs[ws] != tab || selection.Pane != pane {
			t.Fatalf("%s selection=%+v", label, selection)
		}
	}
	check(a, "A before", 2, 2, 2, func() bool { return true })
	check(b, "B before", 3, 3, 3, func() bool { return true })
	// The canonical space is s3. p1 exits naturally while both displays look elsewhere.
	apiOK(t, dir, api.MethodPaneSendInput, api.PaneSendInputParams{PaneID: "p1", Text: "exit", Keys: []string{"Enter"}}, nil)
	check(a, "A after hidden exit", 2, 2, 2, func() bool { return len(a.lastInput.State.Workspaces) == 2 })
	check(b, "B after hidden exit", 3, 3, 3, func() bool { return len(b.lastInput.State.Workspaces) == 2 })
	// Reorder changes only indices, including the canonical selection's index.
	apiOK(t, dir, "workspace.move", map[string]any{"workspace_id": "s3", "insert_index": 0}, nil)
	check(a, "A after reorder", 2, 2, 2, func() bool { return a.lastInput.State.Workspaces[0].ID == 3 })
	check(b, "B after reorder", 3, 3, 3, func() bool { return b.lastInput.State.Workspaces[0].ID == 3 })
	// Explicit focus must still reach both viewers, including an unchanged canonical target.
	apiOK(t, dir, api.MethodPaneFocus, api.PaneTarget{PaneID: "p3"}, nil)
	check(a, "A explicit focus", 3, 3, 3, func() bool { return a.lastInput.State.ActiveWorkspace().ID == 3 })
	check(b, "B explicit focus", 3, 3, 3, func() bool { return b.lastInput.State.ActiveWorkspace().ID == 3 })
	// Restore separate views after a genuine socket reconnect.
	a.conn.Close()
	a = dialClient(t, dir)
	defer a.conn.Close()
	a.send(t, clientMsg{Type: "hello", Cols: 120, Rows: 40, Selection: &endpoint.ViewSelection{Workspace: 2, Tabs: map[int]int{2: 2}, Pane: 2}})
	check(a, "A reconnect", 2, 2, 2, func() bool { return true })
	// A visible final pane exits: only its viewer falls back to the first surviving space.
	apiOK(t, dir, api.MethodPaneSendInput, api.PaneSendInputParams{PaneID: "p2", Text: "exit", Keys: []string{"Enter"}}, nil)
	check(a, "A visible exit fallback", 3, 3, 3, func() bool { return len(a.lastInput.State.Workspaces) == 1 })
	check(b, "B other view survives", 3, 3, 3, func() bool { return len(b.lastInput.State.Workspaces) == 1 })
}
