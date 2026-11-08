package tui

import (
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/teststate"
	"testing"
)

func TestIdleDeleteRejectsChangedPTYsAndReusedID(t *testing.T) {
	for _, kind := range []string{"input", "state", "reuse", "unchanged"} {
		t.Run(kind, func(t *testing.T) {
			teststate.Lock(t)
			a := specRuntimeApp(t)
			if err := a.StartInitialPane(); err != nil {
				t.Fatal(err)
			}
			a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
			id := a.st.FocusedPane()
			a.SetReported(id, "codex", "idle")
			list := a.IdlePanes()
			if len(list.Panes) != 1 {
				t.Fatalf("candidates: %+v", list)
			}
			candidate := list.Panes[0]
			switch kind {
			case "input":
				a.sup.Input(id, []byte("x"))
			case "state":
				a.SetReported(id, "codex", "blocked")
			case "reuse":
				a.exec(model.CmdClosePane{Pane: id})
				a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
				if a.st.FocusedPane() != id {
					t.Fatal("pane ID was not reused")
				}
				a.SetReported(id, "codex", "idle")
			}
			result := a.DeleteIdlePanes([]endpoint.IdlePane{candidate})
			if kind == "unchanged" {
				if result.Deleted != 1 || result.Failed != 0 || a.sup.Get(id) != nil {
					t.Fatalf("delete: %+v", result)
				}
			} else if result.Deleted != 0 || result.Failed != 1 || a.sup.Get(id) == nil {
				t.Fatalf("stale delete: %+v", result)
			}
		})
	}
}
func TestIdlePanesKeepsLastPane(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.SetReported(a.st.FocusedPane(), "codex", "idle")
	list := a.IdlePanes()
	if !list.LastPaneKept || len(list.Panes) != 0 {
		t.Fatalf("last pane: %+v", list)
	}
}

func TestIdleWorkingReportUpdatesActivity(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	id := a.st.FocusedPane()
	p := a.sup.Get(id)
	before, seq := p.IdleActivity()
	a.SetReported(id, "codex", "working")
	after, n := p.IdleActivity()
	if after.Before(before) || n != seq+1 {
		t.Fatalf("working activity: %v %d", after, n)
	}
	a.SetReported(id, "codex", "idle")
	last, n := p.IdleActivity()
	if !last.Equal(after) || n != seq+1 {
		t.Fatalf("idle report changed activity: %v %d", last, n)
	}
}

func TestIdleDeleteKeepsLastPaneAfterBatch(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	for _, id := range a.st.Workspaces[0].Tabs[0].Layout.PaneIDs() {
		a.SetReported(id, "codex", "idle")
	}
	list := a.IdlePanes()
	if len(list.Panes) != 2 {
		t.Fatalf("candidates: %+v", list)
	}
	result := a.DeleteIdlePanes(list.Panes)
	if result.Deleted != 1 || result.Failed != 1 || a.sup.Get(list.Panes[1].ID) == nil || a.quit {
		t.Fatalf("last pane batch: %+v", result)
	}
}
