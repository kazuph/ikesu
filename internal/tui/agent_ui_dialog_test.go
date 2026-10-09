package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/teststate"
	"testing"
)

func TestG5DialogEditingCursorInMiddleOfUnicodeName(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.st.Workspaces[0].Name = "a界b"
	a.renameWorkspaceDialog(0)
	if a.dialog.d.InputCursor != 3 {
		t.Fatal("initial cursor")
	}
	a.dialogKey(uv.Key{Code: uv.KeyLeft})
	a.dialogKey(uv.Key{Code: 'x', Text: "x"})
	if a.dialog.d.Input != "a界xb" || a.dialog.d.InputCursor != 3 {
		t.Fatal(a.dialog.d)
	}
	a.dialogKey(uv.Key{Code: uv.KeyBackspace})
	a.dialogKey(uv.Key{Code: uv.KeyDelete})
	if a.dialog.d.Input != "a界" || a.dialog.d.InputCursor != 2 {
		t.Fatal(a.dialog.d)
	}
	a.dialogKey(uv.Key{Code: uv.KeyHome})
	a.dialogKey(uv.Key{Code: 'y', Text: "y"})
	a.dialogKey(uv.Key{Code: uv.KeyEnter})
	if a.st.Workspaces[0].Name != "ya界" {
		t.Fatal(a.st.Workspaces[0].Name)
	}
}
