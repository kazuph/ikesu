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
