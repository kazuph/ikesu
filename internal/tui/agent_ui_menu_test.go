package tui

import (
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/teststate"
	"reflect"
	"testing"
)

func menuIndex(a *App, label string) int {
	for i, item := range a.menu.m.Items {
		if item.Label == label {
			return i
		}
	}
	return -1
}

func TestG5MenuAgentPresetTargetsClickedPaneAndRegisters(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	var cfg config.Config
	cfg.AgentStart.Commands = map[string][]string{"codex": {"/bin/sh", "-c", "printf 'PRESET_STARTED'; exec sleep 60"}}
	a.SetSidebarConfig(cfg, false)
	a.openWorkspaceMenu(1, 1, 0)
	got := []string{}
	for _, item := range a.menu.m.Items[:3] {
		got = append(got, item.Label)
	}
	if !reflect.DeepEqual(got, []string{"New Claude Code agent", "New Codex agent", "New agy agent"}) {
		t.Fatal(got)
	}
	a.openPaneMenu(50, 5, 1)
	a.activateMenu(menuIndex(a, "New Codex agent"))
	tab := a.st.ActiveWorkspace().TabRef()
	if tab.Layout.Focused() != 4 {
		t.Fatal("new pane focus", tab.Layout.Focused())
	}
	root := tab.Layout.Root()
	if root.First == nil || root.First.First == nil || root.First.First.Leaf != 1 || root.First.Second.Leaf != 4 {
		t.Fatal("wrong split target", root)
	}
	p := a.sup.Get(4)
	if p == nil {
		t.Fatal("missing process")
	}
	specWaitText(t, p, "PRESET_STARTED")
	if !reflect.DeepEqual(p.SpawnArgv(), cfg.AgentStart.Commands["codex"]) {
		t.Fatal(p.SpawnArgv())
	}
	if agent, _, ok := a.Reported(4); !ok || agent != "codex" {
		t.Fatal("agent target not registered")
	}
}

func TestG5PaneMenuRootSideMovesAndSinglePaneExcluded(t *testing.T) {
	teststate.Lock(t)
	for _, tc := range []struct {
		label string
		dir   layout.Direction
		first bool
	}{
		{"Move to left split", layout.Vertical, true}, {"Move to right split", layout.Vertical, false},
		{"Move to upper split", layout.Horizontal, true}, {"Move to lower split", layout.Horizontal, false},
	} {
		a := specRuntimeApp(t)
		a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
		a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
		a.openPaneMenu(30, 8, 2)
		a.activateMenu(menuIndex(a, tc.label))
		l := a.st.ActiveWorkspace().TabRef().Layout
		root := l.Root()
		if root.Direction != tc.dir || l.Focused() != 2 {
			t.Fatal(tc, root, l.Focused())
		}
		target, other := root.Second, root.First
		if tc.first {
			target, other = root.First, root.Second
		}
		if target.Leaf != 2 || other.First.Leaf != 1 || other.Second.Leaf != 3 {
			t.Fatal("root order", tc, root)
		}
	}
	a := specRuntimeApp(t)
	a.openPaneMenu(30, 8, 1)
	for _, label := range []string{"Move to left split", "Move to right split", "Move to upper split", "Move to lower split", "Equalize pane sizes", "Cycle pane layout", "Rotate panes", "Rotate panes reverse"} {
		if menuIndex(a, label) >= 0 {
			t.Fatal("single pane item", label)
		}
	}
}
