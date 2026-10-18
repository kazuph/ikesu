package tui

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestStage6bHelpEffectiveKeys(t *testing.T) {
	c, err := config.Decode([]byte("[keys]\nnavigate_workspace_up='u'\nnavigate_workspace_down='d'\nnavigate_pane_left='a'\nnavigate_pane_down='s'\nnavigate_pane_up='w'\nnavigate_pane_right='f'\n[[keys.command]]\nkey='prefix+g'\ntype='pane'\ncommand='pwd'\n"))
	if err != nil {
		t.Fatal(err)
	}
	a := &App{sidebarConfig: c}
	want := map[string]string{"session navigator": "unset", "workspace list": "u / d", "move focus": "a / s / w / f / left / right", "custom command": "prefix+g"}
	for _, row := range a.helpRows() {
		if bind, ok := want[row.Cmd]; ok {
			if row.Bind != bind {
				t.Fatalf("%s = %q, want %q", row.Cmd, row.Bind, bind)
			}
			delete(want, row.Cmd)
		}
	}
	if len(want) != 0 {
		t.Fatal("missing rows", want)
	}
}

func TestStage6bHelpKeys(t *testing.T) {
	for _, height := range []int{45, 18} {
		a := &App{st: model.New("", 160, height)}
		a.openHelp()
		a.helpKey(uv.Key{Code: uv.KeyPgDown})
		if a.help.scroll != 8 {
			t.Fatalf("height %d: page = %d", height, a.help.scroll)
		}
		a.helpKey(uv.Key{Code: uv.KeyEnd})
		if a.help.scroll != a.helpMaxScroll() {
			t.Fatal("End must reach last row")
		}
		a.helpKey(uv.Key{Code: uv.KeyHome})
		if a.help.scroll != 0 {
			t.Fatal("Home must reach first row")
		}
		a.helpKey(uv.Key{Code: 'q', Text: "q"})
		if a.help == nil {
			t.Fatal("q must keep help open")
		}
		a.helpKey(uv.Key{Code: '?', Text: "?"})
		if a.help != nil {
			t.Fatal("? must close help")
		}
	}
}

func TestStage6bNavigatorConditions(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.navigator = &navigatorView{query: "bogus"}
	a.navigatorKey(uv.Key{Code: 'a'})
	if a.navigator.query != "" {
		t.Fatal("all must clear query")
	}
	a.navigatorKey(uv.Key{Code: 'b'})
	a.navigatorKey(uv.Key{Code: '/'})
	if a.navigator.filter != "" || !a.navigator.searching {
		t.Fatal("search must clear state filter")
	}
	a.navigatorKey(uv.Key{Code: 'x', Text: "bogus"})
	a.navigatorKey(uv.Key{Code: uv.KeyEscape})
	a.navigatorKey(uv.Key{Code: 'b'})
	if a.navigator.query != "" || a.navigator.filter != "blocked" {
		t.Fatal("state must clear search")
	}
	a.navigatorKey(uv.Key{Code: uv.KeyBackspace})
	if a.navigator.filter != "" {
		t.Fatal("Backspace must clear filter")
	}
}

func TestStage6bNavigatorMovementAndCollapse(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.openNavigator()
	a.navigatorKey(uv.Key{Code: uv.KeyHome})
	a.navigatorKey(uv.Key{Code: ' '})
	rows, _ := a.navigatorRows()
	if len(rows) != 1 || rows[0].Pane != -1 {
		t.Fatal("workspace must collapse", rows)
	}
	a.navigatorKey(uv.Key{Code: ' '})
	rows, _ = a.navigatorRows()
	if len(rows) != 2 {
		t.Fatal("workspace must expand", rows)
	}
	a.navigatorKey(uv.Key{Code: 'd', Mod: uv.ModCtrl})
	if a.navigator.selected != 1 {
		t.Fatal("half page must clamp to end")
	}
	a.navigatorKey(uv.Key{Code: 'u', Mod: uv.ModCtrl})
	if a.navigator.selected != 0 {
		t.Fatal("half page must clamp to start")
	}
	a.navigatorKey(uv.Key{Code: 'g', Mod: uv.ModShift, Text: "G"})
	if a.navigator.selected != 1 {
		t.Fatal("G must select last row")
	}
	a.navigatorKey(uv.Key{Code: '/'})
	a.navigatorKey(uv.Key{Code: 'p', Mod: uv.ModCtrl})
	if a.navigator.selected != 0 {
		t.Fatal("search Ctrl-P must move up")
	}
	a.navigatorKey(uv.Key{Code: 'n', Mod: uv.ModCtrl})
	if a.navigator.selected != 1 || a.navigator.query != "" {
		t.Fatal("search Ctrl-N must move without typing")
	}
}
