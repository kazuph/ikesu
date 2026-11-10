package tui

import (
	"testing"

	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/teststate"
)

func menuLabels(a *App) []string {
	var got []string
	for _, item := range a.menu.m.Items {
		got = append(got, item.Label)
	}
	return got
}

// 右クリックのタブメニューから、タブを左右と別のスペースへ移せる。
func TestTabMenuMovesTabLeftRightAndToAnotherSpace(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.exec(model.CmdNewTab{Workspace: 0})
	ws := a.st.Workspaces[0]
	first, second := ws.Tabs[0].ID, ws.Tabs[1].ID
	a.openTabMenu(10, 0, 0, 0)
	if !a.menu.m.Items[menuIndex(a, "Move tab left")].Disabled || a.menu.m.Items[menuIndex(a, "Move to space…")].Disabled == false {
		t.Fatal("first tab: left must be off; one space: space move must be off", menuLabels(a))
	}
	a.activateMenu(menuIndex(a, "Move tab right"))
	if ws.Tabs[0].ID != second || ws.Tabs[1].ID != first {
		t.Fatal("move right", ws.Tabs[0].ID, ws.Tabs[1].ID)
	}
	a.openTabMenu(10, 0, 0, 1)
	a.activateMenu(menuIndex(a, "Move tab left"))
	if ws.Tabs[0].ID != first {
		t.Fatal("move left")
	}

	a.exec(model.CmdNewWorkspace{Cwd: t.TempDir(), Name: "dest"})
	destID := a.st.Workspaces[1].ID
	a.exec(model.CmdSelectWorkspace{Index: 0})
	pane := ws.Tabs[1].Layout.PaneIDs()[0]
	a.openTabMenu(10, 0, 0, 1)
	a.activateMenu(menuIndex(a, "Move to space…"))
	if a.menu == nil || a.menu.kind != menuMove || len(a.menu.m.Items) != 1 {
		t.Fatal("space list", a.menu)
	}
	a.activateMenu(0)
	dest := a.st.Workspaces[a.wsIndexByID(destID)]
	if len(ws.Tabs) != 1 || len(dest.Tabs) != 2 || dest.Tabs[1].ID != second || a.sup.Get(int(pane)) == nil {
		t.Fatal("tab must move with its running pane", len(ws.Tabs), len(dest.Tabs))
	}
	if a.st.ActiveWorkspace() != dest || dest.ActiveTab != 1 {
		t.Fatal("focus follows the moved tab")
	}
}

// 最後のタブを別のスペースへ移すと、空になったスペースは消える。
func TestTabMoveRemovesEmptiedSpace(t *testing.T) {
	st := model.New("/a", 80, 24)
	st.Apply(model.CmdNewWorkspace{Cwd: "/b"})
	st.Apply(model.CmdNewWorkspace{Cwd: "/c"})
	keep := st.Workspaces[2].ID
	st.Apply(model.CmdSelectWorkspace{Index: 2})
	st.Apply(model.CmdMoveTabToWorkspace{Workspace: 0, Index: 0, Dest: 1})
	if len(st.Workspaces) != 2 || len(st.Workspaces[0].Tabs) != 2 || st.ActiveWorkspace().ID != keep {
		t.Fatal("emptied space must go and the active space stay", len(st.Workspaces))
	}
}

// ペインのメニューから、同じスペースの別のタブ・新しいタブ・別のスペースのタブへ移せる。
func TestPaneMenuMovesPaneToTabNewTabAndOtherSpace(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	p1 := a.focusedPane()
	a.exec(model.CmdNewTab{Workspace: 0})
	p2 := a.focusedPane()
	ws := a.st.Workspaces[0]

	// 別のタブへ: p2 のタブは空になって消え、p1 のタブに2つ並ぶ。
	a.openPaneMenu(10, 5, p2)
	a.activateMenu(menuIndex(a, "Move to tab…"))
	if got := menuLabels(a); len(got) != 1 || got[0] != "tab 1" {
		t.Fatal("tab list (alone in its tab: no New tab)", got)
	}
	a.activateMenu(0)
	if len(ws.Tabs) != 1 || ws.Tabs[0].Layout.PaneCount() != 2 || a.sup.Get(p2) == nil {
		t.Fatal("pane must join the other tab", len(ws.Tabs))
	}

	// 新しいタブへ。
	a.openPaneMenu(10, 5, p2)
	a.activateMenu(menuIndex(a, "Move to tab…"))
	a.activateMenu(menuIndex(a, "New tab"))
	if len(ws.Tabs) != 2 || !ws.Tabs[1].Layout.HasPane(model.PaneID(p2)) {
		t.Fatal("pane must get a new tab")
	}

	// 別のスペースのタブへ: スペースを選び、次にタブを選ぶ。
	a.exec(model.CmdNewWorkspace{Cwd: t.TempDir(), Name: "dest"})
	destID := a.st.Workspaces[1].ID
	a.openPaneMenu(10, 5, p1)
	a.activateMenu(menuIndex(a, "Move pane to space…"))
	if got := menuLabels(a); len(got) != 1 {
		t.Fatal("space list", got)
	}
	a.activateMenu(0)
	if got := menuLabels(a); len(got) != 2 {
		t.Fatal("dest tab list", got)
	}
	a.activateMenu(0)
	dest := a.st.Workspaces[a.wsIndexByID(destID)]
	if dest.Tabs[0].Layout.PaneCount() != 2 || !dest.Tabs[0].Layout.HasPane(model.PaneID(p1)) || a.sup.Get(p1) == nil {
		t.Fatal("pane must move into the other space's tab")
	}
}

// スペースに1つだけのペインを、同じスペースの新しいタブへ移す命令は、スペースを壊さずに断る。
func TestPaneAloneInSpaceRefusesNewTabInSameSpace(t *testing.T) {
	st := model.New("/a", 80, 24)
	pid := st.Workspaces[0].Tabs[0].Layout.PaneIDs()[0]
	out := &model.PaneMoveOutcome{}
	st.Apply(model.CmdMovePane{Pane: pid, Dest: model.MoveDest{Kind: model.MoveDestNewTab, Workspace: 0}, Out: out})
	if out.Changed || out.Reason != "same_tab" || len(st.Workspaces) != 1 || !st.Workspaces[0].Tabs[0].Layout.HasPane(pid) {
		t.Fatal("must refuse and keep the space", out, len(st.Workspaces))
	}
}

// タブに1つだけのペインの「Move to tab…」には、同じスペースの「New tab」を出さない。
func TestPaneMenuHidesNewTabWhenAloneInTab(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	p1 := a.focusedPane()
	a.exec(model.CmdNewTab{Workspace: 0})
	a.openPaneMenu(10, 5, p1)
	a.activateMenu(menuIndex(a, "Move to tab…"))
	if got := menuLabels(a); len(got) != 1 || got[0] != "tab 2" {
		t.Fatal("only the other tab", got)
	}
}
