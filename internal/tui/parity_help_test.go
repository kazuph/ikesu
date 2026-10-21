package tui

import (
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/model"
)

func TestParityHelpPageUsesObservedEightRows(t *testing.T) {
	a := &App{st: model.New("", 160, 45)}
	a.openHelp()
	if a.helpBody() != 15 {
		t.Fatalf("distribution viewport contains 15 command rows, got %d", a.helpBody())
	}
	a.helpKey(uv.Key{Code: uv.KeyPgDown})
	if a.help.scroll != 8 {
		t.Fatalf("distribution PgDown advances 8 rows, got %d", a.help.scroll)
	}
	a.helpKey(uv.Key{Code: uv.KeyPgUp})
	if a.help.scroll != 0 {
		t.Fatalf("PgUp did not return to the start: %d", a.help.scroll)
	}
}
