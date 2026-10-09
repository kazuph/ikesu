package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/teststate"
	"testing"
)

func TestParityNavigatorSearchEscapeAndFocus(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	focused := a.focusedPane()
	specFeed(a, "\x02g")
	if a.navigator == nil {
		t.Fatal("prefix g must open navigator")
	}
	rows, count := a.navigatorRows()
	if count != 2 || len(rows) != 3 || rows[a.navigator.selected].Pane != focused {
		t.Fatal("must select focused pane", rows)
	}
	specFeed(a, "/pane 1")
	rows, _ = a.navigatorRows()
	if len(rows) != 2 || rows[1].Pane != 1 {
		t.Fatal("search must filter panes", rows)
	}
	a.navigatorKey(uv.Key{Code: uv.KeyEscape})
	if a.navigator == nil || a.navigator.searching || a.navigator.query != "pane 1" {
		t.Fatal("escape leaves the query and navigator")
	}
	if a.navigator.selected != 1 {
		t.Fatal("search clamps the existing selection to the matching pane")
	}
	a.navigatorKey(uv.Key{Code: uv.KeyEnter})
	if a.navigator != nil || a.focusedPane() != 1 {
		t.Fatal("enter must focus matching pane")
	}
}

func TestParityNavigatorEmptyFilterDoesNotChangeFocus(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	before := a.focusedPane()
	a.openNavigator()
	a.navigatorKey(uv.Key{Code: 'w'})
	rows, count := a.navigatorRows()
	if len(rows) != 0 || count != 1 {
		t.Fatal("working filter must exclude unknown shell", rows, count)
	}
	a.navigatorKey(uv.Key{Code: uv.KeyEnter})
	if a.navigator == nil || a.focusedPane() != before {
		t.Fatal("empty list must not route focus")
	}
	a.navigatorKey(uv.Key{Code: uv.KeyEscape})
	if a.navigator != nil {
		t.Fatal("normal escape must close navigator")
	}
}
