package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/teststate"
	"testing"
)

func TestParityCopySearchEscapeClearsBeforeExiting(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.enterCopy(false)
	a.cm.query = "PARITY"
	a.copyFindAll()
	a.copyKey(uv.Key{Code: uv.KeyEscape}, nil)
	if a.cm == nil || a.cm.query != "" || len(a.cm.matches) != 0 {
		t.Fatal("Escape must clear search and keep copy mode")
	}
	a.copyKey(uv.Key{Code: 'c', Mod: uv.ModCtrl}, nil)
	if a.cm == nil {
		t.Fatal("distribution ignores Ctrl-C inside copy mode")
	}
	a.copyKey(uv.Key{Code: uv.KeyEscape}, nil)
	if a.cm != nil {
		t.Fatal("Escape without search must exit copy mode")
	}
}
