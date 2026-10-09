package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/teststate"
	"testing"
)

func TestParityNameInputAppendsUnicodeAndClearsWithControlC(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.st.Workspaces[0].Name = "a界b"
	a.renameWorkspaceDialog(0)
	if a.dialog.d.InputCursor != 3 {
		t.Fatal("initial cursor")
	}
	a.dialogKey(uv.Key{Code: uv.KeyLeft})
	a.dialogKey(uv.Key{Code: 'x', Text: "x"})
	if a.dialog.d.Input != "a界bx" || a.dialog.d.InputCursor != 4 {
		t.Fatal(a.dialog.d)
	}
	a.dialogKey(uv.Key{Code: uv.KeyBackspace})
	a.dialogKey(uv.Key{Code: uv.KeyDelete})
	if a.dialog.d.Input != "a界b" || a.dialog.d.InputCursor != 3 {
		t.Fatal(a.dialog.d)
	}
	a.dialogKey(uv.Key{Code: uv.KeyHome})
	a.dialogKey(uv.Key{Code: 'y', Text: "y"})
	a.dialogKey(uv.Key{Code: 'c', Mod: uv.ModCtrl})
	if a.dialog.d.Input != "" || a.dialog.d.InputCursor != 0 {
		t.Fatal("Ctrl-C must clear the name without closing the input")
	}
	a.dialogKey(uv.Key{Code: 'y', Text: "a界by"})
	a.dialogKey(uv.Key{Code: uv.KeyEnter})
	if a.st.Workspaces[0].Name != "a界by" {
		t.Fatal(a.st.Workspaces[0].Name)
	}
}
