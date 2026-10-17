package tui

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/teststate"
)

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
