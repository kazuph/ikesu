package tui

import (
	"testing"

	"github.com/kazuph/ikesu/internal/detect"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/status"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestReusedPaneDoesNotInheritDetectedStatus(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	id := a.st.FocusedPane()
	a.trackers[id] = &status.Tracker{AgentID: "codex", State: detect.Blocked, Seen: true}
	if got := a.PaneStatusKey(id); got != "blocked" {
		t.Fatalf("old status %s", got)
	}
	a.exec(model.CmdClosePane{Pane: id})
	a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	if a.st.FocusedPane() != id {
		t.Fatal("pane number was not reused")
	}
	if got := a.PaneStatusKey(id); got != "unknown" {
		t.Fatalf("new pane inherited old status %q", got)
	}
}

func TestReusedPaneDoesNotInheritFocusHistory(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	a.exec(model.CmdFocusPane{Pane: 1})
	a.exec(model.CmdClosePane{Pane: 2})
	a.exec(model.CmdSplitPane{Pane: 1, Dir: layout.Vertical, Ratio: 0.5, Focus: false})
	if a.st.FocusedPane() != 1 || a.sup.Get(2) == nil {
		t.Fatal("replacement setup failed")
	}
	a.focusHistoryStep(false)
	if got := a.st.FocusedPane(); got != 1 {
		t.Fatalf("focus history selected replacement pane p%d", got)
	}
}
