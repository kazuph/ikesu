package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/api"
)

// apiReq sends one request and returns the decoded response.
func apiReq(t *testing.T, dir, method string, params any) api.Response {
	t.Helper()
	conn, err := net.Dial("unix", api.APISocketPath(dir))
	if err != nil {
		t.Fatalf("dial api: %v", err)
	}
	defer conn.Close()
	if err := api.WriteRequest(conn, api.Request{
		ID:     "t:" + method,
		Method: method,
		Params: api.MarshalParams(params),
	}); err != nil {
		t.Fatalf("write %s: %v", method, err)
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Fatalf("read %s: %v", method, err)
	}
	var resp api.Response
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("decode %s: %v", method, err)
	}
	return resp
}

func apiOK(t *testing.T, dir, method string, params any, out any) {
	t.Helper()
	resp := apiReq(t, dir, method, params)
	if resp.Err != nil {
		t.Fatalf("%s: %+v", method, resp.Err)
	}
	if out != nil {
		if err := json.Unmarshal(resp.Result, out); err != nil {
			t.Fatalf("%s result: %v", method, err)
		}
	}
}

func firstPaneID(t *testing.T, dir string) string {
	t.Helper()
	var list api.PaneListResult
	apiOK(t, dir, api.MethodPaneList, api.PaneListParams{}, &list)
	if len(list.Panes) == 0 {
		t.Fatal("no panes")
	}
	return list.Panes[0].PaneID
}

func TestWorkspaceAPI(t *testing.T) {
	dir, done := startTestServer(t)
	dialClient(t, dir)
	defer stopServer(t, dir, done)

	var list api.WorkspaceListResult
	apiOK(t, dir, api.MethodWorkspaceList, api.EmptyParams{}, &list)
	if len(list.Workspaces) != 1 {
		t.Fatalf("workspaces = %d, want 1", len(list.Workspaces))
	}
	wsID := list.Workspaces[0].WorkspaceID
	if !list.Workspaces[0].Focused {
		t.Fatal("first workspace should be focused")
	}

	// Create without focus: count grows, focus stays.
	var created api.WorkspaceCreatedResult
	apiOK(t, dir, api.MethodWorkspaceCreate,
		api.WorkspaceCreateParams{Label: strptr("second")}, &created)
	apiOK(t, dir, api.MethodWorkspaceList, api.EmptyParams{}, &list)
	if len(list.Workspaces) != 2 {
		t.Fatalf("workspaces = %d, want 2", len(list.Workspaces))
	}
	if !list.Workspaces[0].Focused {
		t.Fatal("workspace.create without focus should not steal focus")
	}
	if created.Workspace.Label != "second" {
		t.Fatalf("label = %q", created.Workspace.Label)
	}

	// Rename and focus the new workspace.
	newID := created.Workspace.WorkspaceID
	var info api.WorkspaceInfoResult
	apiOK(t, dir, api.MethodWorkspaceRename,
		api.WorkspaceRenameParams{WorkspaceID: newID, Label: "renamed"}, &info)
	if info.Workspace.Label != "renamed" {
		t.Fatalf("label = %q", info.Workspace.Label)
	}
	apiOK(t, dir, api.MethodWorkspaceFocus, api.WorkspaceTarget{WorkspaceID: newID}, &info)
	if !info.Workspace.Focused {
		t.Fatal("workspace.focus did not focus")
	}

	// Close it; focus returns to the first.
	apiOK(t, dir, api.MethodWorkspaceClose, api.WorkspaceTarget{WorkspaceID: newID}, nil)
	apiOK(t, dir, api.MethodWorkspaceList, api.EmptyParams{}, &list)
	if len(list.Workspaces) != 1 || !list.Workspaces[0].Focused {
		t.Fatalf("after close: %+v", list.Workspaces)
	}

	// Errors.
	resp := apiReq(t, dir, api.MethodWorkspaceGet, api.WorkspaceTarget{WorkspaceID: "w99"})
	if resp.Err == nil || resp.Err.Code != api.ErrWorkspaceNotFound {
		t.Fatalf("workspace.get w99: %+v", resp.Err)
	}
	_ = wsID
}

func TestPaneAPISplitReadSend(t *testing.T) {
	dir, done := startTestServer(t)
	dialClient(t, dir)
	defer stopServer(t, dir, done)

	p1 := firstPaneID(t, dir)

	// get
	var got api.PaneInfoResult
	apiOK(t, dir, api.MethodPaneGet, api.PaneTarget{PaneID: p1}, &got)
	if got.Pane.PaneID != p1 || !got.Pane.Focused {
		t.Fatalf("pane.get: %+v", got.Pane)
	}
	if got.Pane.WorkspaceID == "" || got.Pane.TabID == "" {
		t.Fatalf("pane.get missing workspace/tab: %+v", got.Pane)
	}

	// current via caller_pane_id
	var cur api.PaneCurrentResult
	apiOK(t, dir, api.MethodPaneCurrent,
		api.PaneCurrentParams{CallerPaneID: &p1}, &cur)
	if cur.Pane.PaneID != p1 {
		t.Fatalf("pane.current = %q, want %q", cur.Pane.PaneID, p1)
	}

	// split right → 2 panes (focus=false by default, like herdr)
	var split api.PaneInfoResult
	apiOK(t, dir, api.MethodPaneSplit,
		api.PaneSplitParams{Direction: "right"}, &split)
	p2 := split.Pane.PaneID
	if p2 == p1 || p2 == "" {
		t.Fatalf("split pane id = %q", p2)
	}
	var list api.PaneListResult
	apiOK(t, dir, api.MethodPaneList, api.PaneListParams{}, &list)
	if len(list.Panes) != 2 {
		t.Fatalf("panes = %d, want 2", len(list.Panes))
	}
	if split.Pane.Focused {
		t.Fatal("split without --focus should not focus the new pane")
	}

	// split down with focus
	var split2 api.PaneInfoResult
	apiOK(t, dir, api.MethodPaneSplit,
		api.PaneSplitParams{Direction: "down", Focus: true}, &split2)
	if !split2.Pane.Focused {
		t.Fatal("split with focus=true should focus the new pane")
	}
	apiOK(t, dir, api.MethodPaneList, api.PaneListParams{}, &list)
	if len(list.Panes) != 3 {
		t.Fatalf("panes = %d, want 3", len(list.Panes))
	}
	// focus back to p1
	apiOK(t, dir, api.MethodPaneFocus, api.PaneFocusParams{PaneID: p1}, &got)
	if !got.Pane.Focused {
		t.Fatal("pane.focus p1 did not focus")
	}

	// rename
	apiOK(t, dir, api.MethodPaneRename,
		api.PaneRenameParams{PaneID: p1, Label: strptr("main")}, &got)
	if got.Pane.Label == nil || *got.Pane.Label != "main" {
		t.Fatalf("label = %+v", got.Pane.Label)
	}

	// send_input echoes in the shell → readable via pane.read
	marker := fmt.Sprintf("MK%d", time.Now().UnixNano()%100000)
	apiOK(t, dir, api.MethodPaneSendInput, api.PaneSendInputParams{
		PaneID: p1, Text: "echo " + marker, Keys: []string{"enter"},
	}, nil)
	waitRead(t, dir, p1, marker)

	// send_text (no enter) shows typed-but-not-executed text on the prompt line
	marker2 := "TAIL" + marker
	apiOK(t, dir, api.MethodPaneSendText,
		api.PaneSendTextParams{PaneID: p1, Text: "echo " + marker2}, nil)
	waitRead(t, dir, p1, marker2)

	// invalid key
	resp := apiReq(t, dir, api.MethodPaneSendKeys, api.PaneSendKeysParams{
		PaneID: p1, Keys: []string{"not-a-real-key-xyz"},
	})
	if resp.Err == nil || resp.Err.Code != api.ErrInvalidKey {
		t.Fatalf("bad key: %+v", resp.Err)
	}
	// valid key
	apiOK(t, dir, api.MethodPaneSendKeys,
		api.PaneSendKeysParams{PaneID: p1, Keys: []string{"ctrl+c"}}, nil)

	// report_agent shows up in pane.get
	apiOK(t, dir, api.MethodPaneReportAgent, api.PaneReportAgentParams{
		PaneID: p1, Agent: "claude", State: "working",
	}, nil)
	apiOK(t, dir, api.MethodPaneGet, api.PaneTarget{PaneID: p1}, &got)
	if got.Pane.Agent == nil || *got.Pane.Agent != "claude" {
		t.Fatalf("agent = %+v", got.Pane.Agent)
	}
	if got.Pane.AgentStatus != "working" {
		t.Fatalf("agent_status = %q", got.Pane.AgentStatus)
	}

	// pane.current via process ancestry: run `sleep` in p1 and resolve by pid.
	marker3 := "SL" + marker
	apiOK(t, dir, api.MethodPaneSendInput, api.PaneSendInputParams{
		PaneID: p1, Text: "sleep 5 # " + marker3, Keys: []string{"enter"},
	}, nil)
	var pi api.PaneProcessInfoResult
	deadline := time.Now().Add(8 * time.Second)
	for {
		apiOK(t, dir, api.MethodPaneProcessInfo, api.PaneQueryParams{PaneID: p1}, &pi)
		found := false
		for _, pr := range pi.ForegroundProcesses {
			if pr.Name == "sleep" {
				found = true
				pid := uint32(pr.PID)
				apiOK(t, dir, api.MethodPaneCurrent,
					api.PaneCurrentParams{CallerProcessID: &pid}, &cur)
				if cur.Pane.PaneID != p1 {
					t.Fatalf("ancestry pane.current = %q, want %q", cur.Pane.PaneID, p1)
				}
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("sleep never became foreground")
		}
		time.Sleep(100 * time.Millisecond)
	}

	// layout/neighbor/edges after the earlier splits
	var lay api.PaneLayoutResult
	apiOK(t, dir, api.MethodPaneLayout, api.PaneQueryParams{PaneID: p1}, &lay)
	if len(lay.Layout.Panes) != 3 {
		t.Fatalf("layout panes = %d, want 3", len(lay.Layout.Panes))
	}
	if len(lay.Layout.Splits) != 2 {
		t.Fatalf("layout splits = %d, want 2", len(lay.Layout.Splits))
	}
	var nb api.PaneNeighborResult
	apiOK(t, dir, api.MethodPaneNeighbor,
		api.PaneNeighborParams{PaneID: p1, Direction: "right"}, &nb)
	if nb.NeighborPaneID == nil {
		t.Fatal("p1 should have a right neighbor")
	}

	// close the extra panes
	apiOK(t, dir, api.MethodPaneClose, api.PaneTarget{PaneID: p2}, nil)
	apiOK(t, dir, api.MethodPaneClose, api.PaneTarget{PaneID: split2.Pane.PaneID}, nil)
	apiOK(t, dir, api.MethodPaneList, api.PaneListParams{}, &list)
	if len(list.Panes) != 1 {
		t.Fatalf("panes = %d, want 1", len(list.Panes))
	}

	// errors
	resp = apiReq(t, dir, api.MethodPaneGet, api.PaneTarget{PaneID: "p999"})
	if resp.Err == nil || resp.Err.Code != api.ErrPaneNotFound {
		t.Fatalf("pane.get p999: %+v", resp.Err)
	}
	resp = apiReq(t, dir, api.MethodPaneCurrent, api.PaneCurrentParams{})
	if resp.Err == nil || resp.Err.Code != api.ErrPaneNotFound {
		t.Fatalf("pane.current outside: %+v", resp.Err)
	}
}

// waitRead polls pane.read until marker appears in recent output.
func waitRead(t *testing.T, dir, paneID, marker string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for {
		var res api.PaneReadResultEnvelope
		apiOK(t, dir, api.MethodPaneRead, api.PaneReadParams{
			PaneID: paneID, Source: "recent", Format: "text",
		}, &res)
		if strings.Contains(res.Read.Text, marker) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("marker %q never appeared in pane %s; got:\n%s", marker, paneID, res.Read.Text)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func strptr(s string) *string { return &s }

// waitAPISocket polls until the API socket accepts a connection.
func waitAPISocket(t *testing.T, dir string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		conn, err := net.Dial("unix", api.APISocketPath(dir))
		if err == nil {
			conn.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("api socket never came up at %s: %v", dir, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Reading a pane works without an attached display client.
func TestPaneReadNoClient(t *testing.T) {
	dir, done := startTestServer(t)
	waitAPISocket(t, dir)
	defer stopServer(t, dir, done)
	// No client attach — pure API usage.
	p1 := firstPaneID(t, dir)
	apiOK(t, dir, api.MethodPaneSendInput, api.PaneSendInputParams{
		PaneID: p1, Text: "echo NOCLIENT-MARKER", Keys: []string{"enter"},
	}, nil)
	waitRead(t, dir, p1, "NOCLIENT-MARKER")
}

func TestTabAPI(t *testing.T) {
	dir, done := startTestServer(t)
	dialClient(t, dir)
	defer stopServer(t, dir, done)

	// The initial workspace has one tab.
	var list api.TabListResult
	apiOK(t, dir, api.MethodTabList, api.TabListParams{}, &list)
	if len(list.Tabs) != 1 {
		t.Fatalf("tabs = %d, want 1", len(list.Tabs))
	}
	tab1 := list.Tabs[0].TabID
	if !strings.HasPrefix(tab1, "w") || !strings.Contains(tab1, ":t") {
		t.Fatalf("tab id %q should look like wN:tM", tab1)
	}
	if !list.Tabs[0].Focused {
		t.Fatal("initial tab should be focused")
	}

	// Create a second tab without focus.
	var created api.TabCreatedResult
	apiOK(t, dir, api.MethodTabCreate, api.TabCreateParams{
		Label: strPtr("shells"),
	}, &created)
	if created.Tab.TabID == tab1 {
		t.Fatal("new tab reuses tab1 id")
	}
	if created.Tab.Label != "shells" {
		t.Fatalf("tab label = %q, want shells", created.Tab.Label)
	}
	if created.Tab.Focused {
		t.Fatal("new tab should not be focused by default")
	}
	if created.RootPane.PaneID == "" {
		t.Fatal("tab create must return a root pane")
	}
	if created.Tab.PaneCount != 1 {
		t.Fatalf("new tab pane_count = %d, want 1", created.Tab.PaneCount)
	}

	// Workspace info reflects the new tab.
	var ws api.WorkspaceInfoResult
	apiOK(t, dir, api.MethodWorkspaceGet,
		api.WorkspaceTarget{WorkspaceID: created.Tab.WorkspaceID}, &ws)
	if ws.Workspace.TabCount != 2 {
		t.Fatalf("tab_count = %d, want 2", ws.Workspace.TabCount)
	}
	if ws.Workspace.ActiveTabID != tab1 {
		t.Fatalf("active_tab_id = %q, want %q", ws.Workspace.ActiveTabID, tab1)
	}

	// Focus the new tab: the workspace's panes change and focus follows.
	tab2 := created.Tab.TabID
	var focused api.TabInfoResult
	apiOK(t, dir, api.MethodTabFocus, api.TabTarget{TabID: tab2}, &focused)
	if !focused.Tab.Focused {
		t.Fatal("focused tab not reported focused")
	}
	apiOK(t, dir, api.MethodWorkspaceGet,
		api.WorkspaceTarget{WorkspaceID: created.Tab.WorkspaceID}, &ws)
	if ws.Workspace.ActiveTabID != tab2 {
		t.Fatalf("active_tab_id = %q, want %q", ws.Workspace.ActiveTabID, tab2)
	}

	// The panes of each tab stay separate: split inside tab2, then check
	// both tabs' pane counts.
	var split api.PaneInfoResult
	apiOK(t, dir, api.MethodPaneSplit, api.PaneSplitParams{
		Direction: "right", TargetPaneID: &created.RootPane.PaneID,
	}, &split)
	var tabs2 api.TabListResult
	apiOK(t, dir, api.MethodTabList, api.TabListParams{}, &tabs2)
	if len(tabs2.Tabs) != 2 {
		t.Fatalf("tabs = %d, want 2", len(tabs2.Tabs))
	}
	counts := map[string]int{}
	for _, tb := range tabs2.Tabs {
		counts[tb.TabID] = tb.PaneCount
	}
	if counts[tab1] != 1 || counts[tab2] != 2 {
		t.Fatalf("pane counts = %v, want tab1=1 tab2=2", counts)
	}
	// The split pane's tab_id is tab2.
	if split.Pane.TabID != tab2 {
		t.Fatalf("split pane tab_id = %q, want %q", split.Pane.TabID, tab2)
	}

	// Rename and get.
	var ren api.TabInfoResult
	apiOK(t, dir, api.MethodTabRename, api.TabRenameParams{
		TabID: tab2, Label: "work",
	}, &ren)
	if ren.Tab.Label != "work" {
		t.Fatalf("renamed label = %q", ren.Tab.Label)
	}
	var got api.TabInfoResult
	apiOK(t, dir, api.MethodTabGet, api.TabTarget{TabID: tab2}, &got)
	if got.Tab.Label != "work" || got.Tab.Number != 2 {
		t.Fatalf("tab get = %+v", got.Tab)
	}

	// Close tab2: its panes die and tab1 is back alone.
	var paneIDs []string
	var pl api.PaneListResult
	apiOK(t, dir, api.MethodPaneList, api.PaneListParams{}, &pl)
	for _, pi := range pl.Panes {
		if pi.TabID == tab2 {
			paneIDs = append(paneIDs, pi.PaneID)
		}
	}
	var okr api.OkResult
	apiOK(t, dir, api.MethodTabClose, api.TabTarget{TabID: tab2}, &okr)
	var tabs3 api.TabListResult
	apiOK(t, dir, api.MethodTabList, api.TabListParams{}, &tabs3)
	if len(tabs3.Tabs) != 1 || tabs3.Tabs[0].TabID != tab1 {
		t.Fatalf("after close tabs = %+v", tabs3.Tabs)
	}
	for _, pid := range paneIDs {
		resp := apiReq(t, dir, api.MethodPaneGet, api.PaneTarget{PaneID: pid})
		if resp.Err == nil || resp.Err.Code != api.ErrPaneNotFound {
			t.Fatalf("closed tab pane %s should be gone: %+v", pid, resp.Err)
		}
	}

	// Unknown tab and malformed ids.
	resp := apiReq(t, dir, api.MethodTabGet, api.TabTarget{TabID: "w1:t99"})
	if resp.Err == nil || resp.Err.Code != api.ErrTabNotFound {
		t.Fatalf("unknown tab: %+v", resp.Err)
	}
	resp = apiReq(t, dir, api.MethodTabGet, api.TabTarget{TabID: "bogus"})
	if resp.Err == nil || resp.Err.Code != api.ErrTabNotFound {
		t.Fatalf("malformed tab: %+v", resp.Err)
	}
}

func TestTabCloseLastRemovesWorkspace(t *testing.T) {
	dir, done := startTestServer(t)
	dialClient(t, dir)
	defer stopServer(t, dir, done)

	// Two workspaces; closing ws2's only tab removes ws2 entirely.
	var created api.WorkspaceCreatedResult
	apiOK(t, dir, api.MethodWorkspaceCreate, api.WorkspaceCreateParams{}, &created)
	ws2 := created.Workspace.WorkspaceID
	tab := created.Tab.TabID

	var okr api.OkResult
	apiOK(t, dir, api.MethodTabClose, api.TabTarget{TabID: tab}, &okr)
	var wl api.WorkspaceListResult
	apiOK(t, dir, api.MethodWorkspaceList, api.EmptyParams{}, &wl)
	for _, w := range wl.Workspaces {
		if w.WorkspaceID == ws2 {
			t.Fatal("closing the last tab should remove its workspace")
		}
	}
	if len(wl.Workspaces) != 1 {
		t.Fatalf("workspaces = %d, want 1", len(wl.Workspaces))
	}
}

func TestTabLayoutSnapshotScoped(t *testing.T) {
	dir, done := startTestServer(t)
	dialClient(t, dir)
	defer stopServer(t, dir, done)

	var created api.TabCreatedResult
	apiOK(t, dir, api.MethodTabCreate, api.TabCreateParams{Focus: true}, &created)
	// Split inside tab2.
	apiOK(t, dir, api.MethodPaneSplit, api.PaneSplitParams{
		Direction: "right", TargetPaneID: &created.RootPane.PaneID,
	}, nil)

	// layout of a tab1 pane shows only tab1's single pane.
	var tl api.TabListResult
	apiOK(t, dir, api.MethodTabList, api.TabListParams{}, &tl)
	tab1 := ""
	for _, tb := range tl.Tabs {
		if tb.TabID != created.Tab.TabID {
			tab1 = tb.TabID
		}
	}
	if tab1 == "" {
		t.Fatal("tab1 not found")
	}
	var pl api.PaneListResult
	apiOK(t, dir, api.MethodPaneList, api.PaneListParams{}, &pl)
	var tab1Pane string
	for _, pi := range pl.Panes {
		if pi.TabID == tab1 {
			tab1Pane = pi.PaneID
		}
	}
	if tab1Pane == "" {
		t.Fatal("no pane found for tab1")
	}
	var lay api.PaneLayoutResult
	apiOK(t, dir, api.MethodPaneLayout, api.PaneQueryParams{PaneID: tab1Pane}, &lay)
	if len(lay.Layout.Panes) != 1 {
		t.Fatalf("tab1 layout panes = %d, want 1", len(lay.Layout.Panes))
	}
	if lay.Layout.TabID != tab1 {
		t.Fatalf("layout tab_id = %q, want %q", lay.Layout.TabID, tab1)
	}
	// Tab2's snapshot has 2 panes.
	apiOK(t, dir, api.MethodPaneLayout,
		api.PaneQueryParams{PaneID: created.RootPane.PaneID}, &lay)
	if len(lay.Layout.Panes) != 2 || lay.Layout.TabID != created.Tab.TabID {
		t.Fatalf("tab2 layout = %d panes in %q", len(lay.Layout.Panes), lay.Layout.TabID)
	}
}

func strPtr(s string) *string { return &s }
