package server

import (
	"testing"

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
