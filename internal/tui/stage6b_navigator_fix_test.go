package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/teststate"
	"testing"
)

func TestStage6bCollapsedNavigatorConditions(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	ids := a.st.Workspaces[0].Tabs[0].Layout.PaneIDs()
	a.st.PaneLabels = map[int]string{ids[0]: "match-first", ids[1]: "second"}
	a.SetReported(ids[0], "codex", "working")
	a.SetReported(ids[1], "codex", "idle")
	a.navigator = &navigatorView{collapsed: map[int]bool{0: true}}
	rows, _ := a.navigatorRows()
	if len(rows) != 1 {
		t.Fatal("initial fold", rows)
	}
	for _, condition := range []string{"query", "filter"} {
		a.navigator.query = ""
		a.navigator.filter = ""
		if condition == "query" {
			a.navigator.query = "match"
		} else {
			a.navigator.filter = "working"
		}
		rows, _ = a.navigatorRows()
		if len(rows) != 2 || rows[1].Pane != ids[0] {
			t.Fatal(condition, rows)
		}
		if !a.navigator.collapsed[0] {
			t.Fatal("condition changed saved fold")
		}
	}
	a.navigator.filter = ""
	rows, _ = a.navigatorRows()
	if len(rows) != 1 || !a.navigator.collapsed[0] {
		t.Fatal("fold not restored", rows)
	}
	a.navigator.query = "match"
	a.navigator.selected = 1
	a.navigatorSelect()
	if a.navigator != nil || a.focusedPane() != ids[0] {
		t.Fatal("matching folded child cannot be selected")
	}
}

func TestStage6bNavigatorKeyContexts(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	for _, searching := range []bool{false, true} {
		for _, mod := range []uv.KeyMod{uv.ModCtrl, uv.ModCtrl | uv.ModShift, uv.ModCtrl | uv.ModAlt} {
			for _, code := range []rune{'n', 'p', 'd', 'u'} {
				a.navigator = &navigatorView{searching: searching, selected: 2}
				a.navigatorKey(uv.Key{Code: code, Mod: mod})
				want := 2
				if mod == uv.ModCtrl && searching && code == 'n' {
					want = 3
				}
				if mod == uv.ModCtrl && searching && code == 'p' {
					want = 1
				}
				if mod == uv.ModCtrl && !searching && code == 'd' {
					want = 3
				}
				if mod == uv.ModCtrl && !searching && code == 'u' {
					want = 0
				}
				if a.navigator.selected != want {
					t.Fatalf("search=%v key=%c mod=%v: %d want %d", searching, code, mod, a.navigator.selected, want)
				}
			}
		}
		for _, code := range []rune{uv.KeyHome, uv.KeyEnd} {
			a.navigator = &navigatorView{searching: searching, selected: 2}
			a.navigatorKey(uv.Key{Code: code})
			want := 2
			if !searching {
				if code == uv.KeyHome {
					want = 0
				} else {
					want = 3
				}
			}
			if a.navigator.selected != want {
				t.Fatalf("search=%v key=%v: %d want %d", searching, code, a.navigator.selected, want)
			}
		}
	}
}
