package server

import (
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/teststate"
	"testing"
)

func TestG2PopupCloseTwiceHiddenOwnerAndTerminalLookupSocket(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	c := dialClient(t, dir)
	defer c.conn.Close()
	defer stopServer(t, dir, done)
	pid := firstPaneID(t, dir)
	var opened api.CommandExecutedResult
	apiOK(t, dir, "pane.command.execute", map[string]any{"pane_id": pid, "command": "exec sleep 60", "action": "popup", "width": 40, "height": 12}, &opened)
	if opened.Type != "command_executed" || opened.Pane != nil || opened.PopupTerminalID == nil {
		t.Fatal(opened)
	}
	target := api.PopupTarget{TerminalID: *opened.PopupTerminalID}
	var info api.PopupTerminalResult
	apiOK(t, dir, "popup.get", target, &info)
	if info.TerminalID != target.TerminalID || info.Scroll.ViewportRows != 10 {
		t.Fatal(info)
	}
	r := apiReq(t, dir, "pane.get", api.PaneTarget{PaneID: target.TerminalID})
	if r.Err == nil {
		t.Fatal("popup resolved as a tile")
	}
	apiOK(t, dir, "workspace.create", api.WorkspaceCreateParams{Focus: true}, nil)
	apiOK(t, dir, "popup.get", target, &info)
	apiOK(t, dir, "popup.scroll", api.PopupScrollParams{TerminalID: target.TerminalID, OffsetFromBottom: 100}, &info)
	if info.Scroll.OffsetFromBottom != 0 {
		t.Fatal(info)
	}
	apiOK(t, dir, "popup.close", api.EmptyParams{}, nil)
	for _, method := range []string{"popup.close", "popup.get"} {
		params := any(api.EmptyParams{})
		if method == "popup.get" {
			params = target
		}
		r = apiReq(t, dir, method, params)
		if r.Err == nil || r.Err.Code != "popup_not_open" || r.Err.Message != "no popup is open" {
			t.Fatal(r)
		}
	}
}
