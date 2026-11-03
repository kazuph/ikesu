package server

import (
	"testing"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestReusedPaneCannotResolvePreviousLabel(t *testing.T) {
	teststate.Lock(t)
	s := newTestServer(t, "label-reuse")
	state := s.app.State()
	state.Apply(model.CmdSplitFocused{Dir: layout.Vertical})
	state.Apply(model.CmdRenamePane{Pane: 2, Label: "reviewer"})
	state.Apply(model.CmdClosePane{Pane: 2})
	state.Apply(model.CmdSplitFocused{Dir: layout.Vertical})
	if state.FocusedPane() != 2 {
		t.Fatal("pane number was not reused")
	}
	if _, err := s.resolveTarget("reviewer", false); err == nil {
		t.Fatal("old label resolved to a new pane")
	}
}

func TestReusedPaneDoesNotInheritReportedMetadata(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	first := firstPaneID(t, dir)
	var split api.PaneInfoResult
	apiOK(t, dir, api.MethodPaneSplit, api.PaneSplitParams{TargetPaneID: &first, Direction: "right"}, &split)
	old := split.Pane.PaneID
	apiOK(t, dir, "pane.report_metadata", map[string]any{"pane_id": old, "source": "test", "tokens": map[string]string{"custom": "old"}}, nil)
	apiOK(t, dir, api.MethodPaneClose, api.PaneTarget{PaneID: old}, nil)
	apiOK(t, dir, api.MethodPaneSplit, api.PaneSplitParams{TargetPaneID: &first, Direction: "right"}, &split)
	if split.Pane.PaneID != old {
		t.Fatalf("got %s, want reused %s", split.Pane.PaneID, old)
	}
	var info api.PaneInfoResult
	apiOK(t, dir, api.MethodPaneGet, api.PaneTarget{PaneID: old}, &info)
	if info.Pane.Tokens["custom"] != "" {
		t.Fatalf("new pane inherited metadata: %v", info.Pane.Tokens)
	}
}
