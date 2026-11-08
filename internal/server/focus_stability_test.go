package server

import (
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/teststate"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPluginZoomedFocuslessDoesNotBroadcast(t *testing.T) {
	teststate.Lock(t)
	dir, done := startPluginServer(t)
	defer stopServer(t, dir, done)
	var second api.WorkspaceCreatedResult
	apiOK(t, dir, api.MethodWorkspaceCreate, api.WorkspaceCreateParams{}, &second)
	root := t.TempDir()
	manifest := "id='focus.zoom'\nname='Zoom'\nversion='0.1.0'\nmin_ikesu_version='0.1.0'\nplatforms=['macos','linux']\n[[panes]]\nid='board'\ntitle='Board'\ncommand=['/bin/cat']\n"
	if err := os.WriteFile(filepath.Join(root, "ikesu-plugin.toml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	apiOK(t, dir, "plugin.link", map[string]any{"path": root}, nil)
	a, b := dialClient(t, dir), dialClient(t, dir)
	defer a.conn.Close()
	defer b.conn.Close()
	a.send(t, clientMsg{Type: "hello", Cols: 120, Rows: 40})
	b.send(t, clientMsg{Type: "hello", Cols: 120, Rows: 40, Selection: &endpoint.ViewSelection{Workspace: 2, Tabs: map[int]int{2: 2}, Pane: 2}})
	for _, c := range []*testConn{a, b} {
		c.readUntil(t, 5*time.Second, func(m clientMsg) bool { return c.lastInput != nil })
	}
	var opened struct {
		PluginPane struct {
			Pane api.PaneInfo `json:"pane"`
		} `json:"plugin_pane"`
	}
	apiOK(t, dir, "plugin.pane.open", map[string]any{"plugin_id": "focus.zoom", "entrypoint": "board", "placement": "zoomed", "target_pane_id": "p1", "focus": false}, &opened)
	for i, c := range []*testConn{a, b} {
		c.readUntil(t, 5*time.Second, func(m clientMsg) bool {
			return c.lastInput != nil && c.lastInput.State.Workspaces[0].Tabs[0].Layout.PaneCount() == 2
		})
		selection := endpoint.CaptureSelection(c.lastInput.State)
		t.Logf("zoomed focus=false viewer%d selection=%+v", i, selection)
		if selection.Workspace != i+1 {
			t.Fatalf("zoomed no-focus broadcast: viewer%d %+v", i, selection)
		}
		targetTab := c.lastInput.State.Workspaces[0].Tabs[0]
		if targetTab.Zoomed == 0 || targetTab.Zoomed != targetTab.Layout.Focused() {
			t.Fatal("zoomed placement weakened")
		}
	}
	// The explicit plugin focus API retains its all-viewer contract.
	apiOK(t, dir, "plugin.pane.focus", map[string]any{"pane_id": opened.PluginPane.Pane.PaneID}, nil)
	b.readUntil(t, 5*time.Second, func(m clientMsg) bool { return b.lastInput != nil && b.lastInput.State.ActiveWorkspace().ID == 1 })
}

func TestFocuslessMoveDissolvesSourceWithTwoViews(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	ready := dialClient(t, dir)
	ready.conn.Close()
	for range 2 {
		apiOK(t, dir, api.MethodWorkspaceCreate, api.WorkspaceCreateParams{Focus: true}, nil)
	}
	a, b := dialClient(t, dir), dialClient(t, dir)
	defer a.conn.Close()
	defer b.conn.Close()
	for i, c := range []*testConn{a, b} {
		id := i + 2
		c.send(t, clientMsg{Type: "hello", Cols: 120, Rows: 40, Selection: &endpoint.ViewSelection{Workspace: id, Tabs: map[int]int{id: id}, Pane: id}})
		c.readUntil(t, 5*time.Second, func(m clientMsg) bool { return c.lastInput != nil })
	}
	// Removing s1 shifts canonical s3's index; destination retains p2 focus.
	split := "right"
	apiOK(t, dir, api.MethodPaneMove, api.PaneMoveParams{PaneID: "p1", Destination: api.PaneMoveDestination{Type: "tab", TabID: "s2:t2", Split: &split}}, nil)
	for i, c := range []*testConn{a, b} {
		c.readUntil(t, 5*time.Second, func(m clientMsg) bool { return c.lastInput != nil && len(c.lastInput.State.Workspaces) == 2 })
		got := endpoint.CaptureSelection(c.lastInput.State)
		if got.Workspace != i+2 || got.Tabs[i+2] != i+2 || got.Pane != i+2 {
			t.Fatalf("move dissolved source and shifted viewer %d: %+v", i, got)
		}
		t.Logf("focusless source-space removal viewer%d=%+v", i, got)
	}
	// A source tab preceding a selected tab dissolves inside s2.
	var newTab api.TabCreatedResult
	apiOK(t, dir, api.MethodTabCreate, api.TabCreateParams{WorkspaceID: strptr("s2")}, &newTab)
	a.conn.Close()
	a = dialClient(t, dir)
	defer a.conn.Close()
	a.send(t, clientMsg{Type: "hello", Cols: 120, Rows: 40, Selection: &endpoint.ViewSelection{Workspace: 2, Tabs: map[int]int{2: 4}, Pane: 4}})
	a.readUntil(t, 5*time.Second, func(m clientMsg) bool { return a.lastInput != nil })
	// First remove p1 from tab2, then move its last p2 so tab2 dissolves.
	for _, pane := range []string{"p1", "p2"} {
		apiOK(t, dir, api.MethodPaneMove, api.PaneMoveParams{PaneID: pane, Destination: api.PaneMoveDestination{Type: "tab", TabID: "s3:t3", Split: &split}}, nil)
	}
	a.readUntil(t, 5*time.Second, func(m clientMsg) bool { return a.lastInput != nil && len(a.lastInput.State.Workspaces[0].Tabs) == 1 })
	got := endpoint.CaptureSelection(a.lastInput.State)
	if got.Workspace != 2 || got.Tabs[2] != 4 || got.Pane != 4 {
		t.Fatalf("source tab removal changed viewer=%+v", got)
	}
	t.Logf("focusless source-tab removal viewer=%+v", got)
}
