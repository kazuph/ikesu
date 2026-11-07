package tui

import (
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/model"
)

func TestOyagoiNavigatorMovesWorkspaceAndKeepsSelectionTarget(t *testing.T) {
	a, _ := worktreeApp(t)
	a.st.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	want := a.st.Workspaces[1].ID
	a.oyagoiWorkspace = want
	a.navigator = &navigatorView{}
	rows, _ := a.navigatorRows()
	if len(rows) == 0 || rows[0].Workspace != 1 || !strings.Contains(rows[0].Name, "oyagoi") {
		t.Fatalf("HQ not first: %+v", rows)
	}
	a.navigator.selected = 0
	a.navigatorSelect()
	if a.st.ActiveWorkspace().ID != want {
		t.Fatal("navigator selected a different workspace")
	}
}
