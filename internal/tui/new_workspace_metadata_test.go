package tui

import (
	"testing"

	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestNewWorkspaceInsertAfterActiveReindexesMetadata(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.exec(model.CmdNewWorkspace{Cwd: a.cwd, Name: "B", NoFocus: true})
	a.exec(model.CmdNewWorkspace{Cwd: a.cwd, Name: "C", NoFocus: true})
	a.st.Active = 0

	a.workspaceNames = map[int]string{0: "name-A", 1: "name-B", 2: "name-C"}
	a.workspaceGit = map[int]render.GitStatus{
		0: {Branch: "branch-A"},
		1: {Branch: "branch-B"},
		2: {Branch: "branch-C"},
	}

	// Insert new workspace right below active (index 0 -> inserted at index 1).
	a.newSectionWorkspace(model.SectionNone)

	if len(a.st.Workspaces) != 4 {
		t.Fatalf("len=%d, want 4", len(a.st.Workspaces))
	}
	if a.st.Active != 1 {
		t.Fatalf("Active=%d, want 1", a.st.Active)
	}
	if a.workspaceNames[0] != "name-A" || a.workspaceNames[1] != "" || a.workspaceNames[2] != "name-B" || a.workspaceNames[3] != "name-C" {
		t.Fatalf("workspaceNames shifted wrongly: %+v", a.workspaceNames)
	}
	if a.workspaceGit[0].Branch != "branch-A" || a.workspaceGit[1].Branch != "" || a.workspaceGit[2].Branch != "branch-B" || a.workspaceGit[3].Branch != "branch-C" {
		t.Fatalf("workspaceGit shifted wrongly: %+v", a.workspaceGit)
	}
}
