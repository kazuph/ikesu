package server

import (
	"bufio"
	"encoding/json"
	"github.com/kazuph/ikesu/internal/teststate"
	"net"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

// splitRight adds one pane via pane.split and returns its id.
func splitRight(t *testing.T, dir string) api.PaneInfoResult {
	t.Helper()
	var res api.PaneInfoResult
	apiOK(t, dir, api.MethodPaneSplit, api.PaneSplitParams{Direction: "right"}, &res)
	return res
}

func TestPaneLayoutSnapshotShape(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	dialClient(t, dir)
	defer stopServer(t, dir, done)

	splitRight(t, dir)
	var res api.PaneLayoutResult
	apiOK(t, dir, api.MethodPaneLayout, api.PaneLayoutParams{}, &res)
	if res.Type != "pane_layout" {
		t.Fatalf("type = %q", res.Type)
	}
	if len(res.Layout.Panes) != 2 {
		t.Fatalf("panes = %d, want 2", len(res.Layout.Panes))
	}
	if len(res.Layout.Splits) != 1 {
		t.Fatalf("splits = %d, want 1", len(res.Layout.Splits))
	}
	sp := res.Layout.Splits[0]
	if sp.ID != "split_0_root" || sp.Direction != "right" || sp.Ratio != 0.5 {
		t.Fatalf("split = %+v", sp)
	}
	r := res.Layout.Panes[0].Rect
	if r.Width <= 0 || r.Height <= 0 {
		t.Fatalf("pane rect = %+v", r)
	}
}

func TestPaneNeighborEdgesFocus(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	dialClient(t, dir)
	defer stopServer(t, dir, done)

	first := firstPaneID(t, dir)
	second := splitRight(t, dir).Pane.PaneID

	var nb api.PaneNeighborEnvelope
	apiOK(t, dir, api.MethodPaneNeighbor, api.PaneNeighborParams{
		PaneID: &first, Direction: "right",
	}, &nb)
	if nb.Neighbor.NeighborPaneID == nil || *nb.Neighbor.NeighborPaneID != second {
		t.Fatalf("right neighbor of %s = %+v", first, nb.Neighbor)
	}

	var nbNone api.PaneNeighborEnvelope
	apiOK(t, dir, api.MethodPaneNeighbor, api.PaneNeighborParams{
		PaneID: &first, Direction: "left",
	}, &nbNone)
	if nbNone.Neighbor.NeighborPaneID != nil {
		t.Fatalf("left neighbor should be none, got %v", *nbNone.Neighbor.NeighborPaneID)
	}

	var edges api.PaneEdgesEnvelope
	apiOK(t, dir, api.MethodPaneEdges, api.PaneEdgesParams{PaneID: &first}, &edges)
	e := edges.Edges
	if !(e.Left && !e.Right && e.Up && e.Down) {
		t.Fatalf("edges(%s) = %+v", first, e)
	}

	var fd api.PaneFocusDirectionEnvelope
	apiOK(t, dir, api.MethodPaneFocusDir, api.PaneFocusDirectionParams{
		PaneID: &first, Direction: "right",
	}, &fd)
	if !fd.Focus.Changed || fd.Focus.FocusedPaneID != second {
		t.Fatalf("focus_direction = %+v", fd.Focus)
	}
}

func TestPaneResizeSwapZoom(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	dialClient(t, dir)
	defer stopServer(t, dir, done)

	first := firstPaneID(t, dir)
	second := splitRight(t, dir).Pane.PaneID

	var rz api.PaneResizeEnvelope
	apiOK(t, dir, api.MethodPaneResize, api.PaneResizeParams{
		PaneID: &first, Direction: "left",
	}, &rz)
	if !rz.Resize.Changed {
		t.Fatalf("resize not applied: %+v", rz.Resize)
	}
	if got := rz.Resize.Layout.Splits[0].Ratio; got != 0.45 {
		t.Fatalf("ratio after resize left = %v, want 0.45", got)
	}

	var sw api.PaneSwapEnvelope
	apiOK(t, dir, api.MethodPaneSwap, api.PaneSwapParams{
		SourcePaneID: &first, TargetPaneID: &second,
	}, &sw)
	if !sw.Swap.Changed || sw.Swap.TargetPaneID == nil || *sw.Swap.TargetPaneID != second {
		t.Fatalf("swap = %+v", sw.Swap)
	}
	if sw.Swap.Layout.Panes[0].PaneID != second {
		t.Fatalf("after swap first slot = %s, want %s",
			sw.Swap.Layout.Panes[0].PaneID, second)
	}

	var zm api.PaneZoomEnvelope
	apiOK(t, dir, api.MethodPaneZoom, api.PaneZoomParams{PaneID: &first, Mode: "on"}, &zm)
	if !zm.Zoom.Changed || !zm.Zoom.Zoomed {
		t.Fatalf("zoom on = %+v", zm.Zoom)
	}
	apiOK(t, dir, api.MethodPaneZoom, api.PaneZoomParams{PaneID: &first, Mode: "on"}, &zm)
	if zm.Zoom.Changed || zm.Zoom.Reason == nil || *zm.Zoom.Reason != "already_zoomed" {
		t.Fatalf("zoom already = %+v", zm.Zoom)
	}
	apiOK(t, dir, api.MethodPaneZoom, api.PaneZoomParams{PaneID: &first, Mode: "off"}, &zm)
	if zm.Zoom.Zoomed {
		t.Fatalf("zoom off still zoomed: %+v", zm.Zoom)
	}
}

func TestPaneArrangeOps(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	dialClient(t, dir)
	defer stopServer(t, dir, done)

	first := firstPaneID(t, dir)
	second := splitRight(t, dir).Pane.PaneID
	third := splitRight(t, dir).Pane.PaneID

	// rotate_forward: leaf order rotates right; tree shape unchanged.
	var arr api.PaneLayoutResult
	apiOK(t, dir, api.MethodPaneArrange, api.PaneArrangeParams{
		PaneID: first, Arrangement: "rotate_forward",
	}, &arr)
	var ex api.LayoutExportResult
	apiOK(t, dir, api.MethodLayoutExport, api.LayoutExportParams{}, &ex)
	root := ex.Layout.Root
	got := [3]string{*root.First.PaneID, *root.Second.First.PaneID, *root.Second.Second.PaneID}
	want := [3]string{third, first, second}
	if got != want {
		t.Fatalf("rotate_forward leaves = %v, want %v", got, want)
	}

	// move_left pulls the focused-order first leaf to a root left split.
	apiOK(t, dir, api.MethodPaneArrange, api.PaneArrangeParams{
		PaneID: first, Arrangement: "move_left",
	}, &arr)
	apiOK(t, dir, api.MethodLayoutExport, api.LayoutExportParams{}, &ex)
	root = ex.Layout.Root
	if *root.First.PaneID != first {
		t.Fatalf("move_left root.first = %v, want %s", root.First.PaneID, first)
	}
	if root.Ratio < 0.3 || root.Ratio > 0.36 {
		t.Fatalf("move_left ratio = %v, want ~1/3", root.Ratio)
	}

	// equalize: root ratio = firstLeaves/total = 0.33.
	apiOK(t, dir, api.MethodPaneArrange, api.PaneArrangeParams{
		PaneID: first, Arrangement: "equalize",
	}, &arr)
	apiOK(t, dir, api.MethodLayoutExport, api.LayoutExportParams{}, &ex)
	if got := ex.Layout.Root.Ratio; got < 0.32 || got > 0.35 {
		t.Fatalf("equalized root ratio = %v", got)
	}
}

func TestLayoutApplyCommandLeaf(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	dialClient(t, dir)
	defer stopServer(t, dir, done)

	cmdLeaf := &api.LayoutNode{
		Type: "pane", Command: []string{"/bin/sh", "-c", "echo ikesuapply1; sleep 30"},
	}
	_ = cmdLeaf
	var ar api.LayoutApplyResult
	apiOK(t, dir, api.MethodLayoutApply, api.LayoutApplyParams{
		Root: &api.LayoutNode{
			Type: "split", Direction: "right", Ratio: 0.4,
			First:  &api.LayoutNode{Type: "pane"},
			Second: cmdLeaf,
		},
		Focus: true,
	}, &ar)
	if ar.Layout.Root.Second.PaneID == nil {
		t.Fatal("apply did not assign pane ids")
	}
	paneID := *ar.Layout.Root.Second.PaneID

	// The command leaf must run its argv, not a login shell.
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var rd api.PaneReadResultEnvelope
		apiOK(t, dir, api.MethodPaneRead, api.PaneReadParams{
			PaneID: paneID, Source: "recent", Format: "text",
		}, &rd)
		if text := rd.Read.Text; text != "" && contains(text, "ikesuapply1") {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("command leaf output never appeared")
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestLayoutSetSplitRatio(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	dialClient(t, dir)
	defer stopServer(t, dir, done)

	splitRight(t, dir)
	var rr api.LayoutSplitRatioSetResult
	apiOK(t, dir, api.MethodLayoutSetSplitRatio, api.LayoutSetSplitRatioParams{
		Path: []bool{}, Ratio: 0.7,
	}, &rr)
	if rr.Layout.Root.Ratio != 0.7 {
		t.Fatalf("ratio = %v, want 0.7", rr.Layout.Root.Ratio)
	}
	// Bad path → split_not_found.
	resp := apiReq(t, dir, api.MethodLayoutSetSplitRatio, api.LayoutSetSplitRatioParams{
		Path: []bool{true, true, true}, Ratio: 0.5,
	})
	if resp.Err == nil || resp.Err.Code != api.ErrSplitNotFound {
		t.Fatalf("bad path: %+v", resp.Err)
	}
}

func TestEventsSubscribeReplayAndWait(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	dialClient(t, dir)
	defer stopServer(t, dir, done)

	// Generate history: a split produces pane_created + layout_updated.
	splitRight(t, dir)

	conn, err := net.Dial("unix", api.APISocketPath(dir))
	if err != nil {
		t.Fatalf("dial api: %v", err)
	}
	defer conn.Close()
	if err := api.WriteRequest(conn, api.Request{
		ID:     "ev",
		Method: api.MethodEventsSubscribe,
		Params: api.MarshalParams(api.EventsSubscribeParams{
			Subscriptions: []api.Subscription{{Type: "pane.created"}, {Type: "layout.updated"}},
		}),
	}); err != nil {
		t.Fatal(err)
	}
	rd := bufio.NewReader(conn)
	line, err := rd.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read subscription_started: %v", err)
	}
	if !contains(string(line), "subscription_started") {
		t.Fatalf("subscribe response = %s", line)
	}
	var sawCreated bool
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !sawCreated {
		conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		line, err := rd.ReadBytes('\n')
		if err != nil {
			continue
		}
		var env api.EventEnvelope
		if json.Unmarshal(line, &env) == nil && env.Event == "pane_created" {
			sawCreated = true
		}
	}
	if !sawCreated {
		t.Fatal("replayed pane_created not received")
	}

	// events.wait: unsupported match → error code.
	resp := apiReq(t, dir, api.MethodEventsWait, api.EventsWaitParams{
		MatchEvent: api.EventMatch{"event": "tab_created"},
	})
	if resp.Err == nil || resp.Err.Code != api.ErrUnsupportedWait {
		t.Fatalf("events.wait tab_created: %+v", resp.Err)
	}
}
