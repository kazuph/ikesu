package tui

import (
	"fmt"
	"testing"

	"github.com/kazuph/ikesu/internal/model"
)

func TestWorkspaceMenuRequestsOyagoiSelection(t *testing.T) {
	a, _ := worktreeApp(t)
	var requested string
	a.machineRequest = func(action string) error { requested = action; return nil }
	ws := a.st.Workspaces[0]
	for _, tc := range []struct{ label, action string }{{"🎏 HQ", "set"}} {
		a.openWorkspaceMenu(1, 1, 0)
		activateLabel(t, a, tc.label)
		want := fmt.Sprintf("oyagoi:%s:%d:%s", tc.action, ws.ID, ws.Generation)
		if requested != want {
			t.Fatalf("%s: got %q, want %q", tc.label, requested, want)
		}
	}
}

func TestWorkspaceSectionFailureKeepsHQ(t *testing.T) {
	a, _ := worktreeApp(t)
	ws := a.st.Workspaces[0]
	a.oyagoiWorkspace, a.oyagoiGeneration = ws.ID, ws.Generation
	before := ws.Section
	a.setWorkspaceMenuSection(-1, model.SectionWork)
	if a.dialog == nil || a.oyagoiWorkspace != ws.ID || a.oyagoiGeneration != ws.Generation || ws.Section != before {
		t.Fatal("invalid workspace changed HQ or its section, or did not show an error")
	}
}

func TestWorkspaceMenuKeepsOpenedIdentityAfterReorder(t *testing.T) {
	for _, label := range []string{"🎏 HQ", "⭐ favorite", "💼 work", "🏠 personal", "No section"} {
		t.Run(label, func(t *testing.T) {
			a, _ := worktreeApp(t)
			a.st.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
			original, other := a.st.Workspaces[0], a.st.Workspaces[1]
			otherSection := other.Section
			var requested string
			a.machineRequest = func(action string) error { requested = action; return nil }
			a.openWorkspaceMenu(1, 1, 0)
			a.st.Workspaces[0], a.st.Workspaces[1] = other, original
			activateLabel(t, a, label)
			if label == "🎏 HQ" {
				if requested != fmt.Sprintf("oyagoi:set:%d:%s", original.ID, original.Generation) {
					t.Fatalf("wrong target: %s", requested)
				}
			} else if other.Section != otherSection || (label != "No section" && (original.Section == model.SectionNone || original.Section == "")) || (label == "No section" && original.Section != model.SectionNone) {
				t.Fatalf("wrong sections: target=%s other=%s", original.Section, other.Section)
			}
		})
	}
}

func TestWorkspaceMenuRejectsRemovedIdentity(t *testing.T) {
	a, _ := worktreeApp(t)
	a.st.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	other := a.st.Workspaces[1]
	otherSection := other.Section
	a.oyagoiWorkspace, a.oyagoiGeneration = other.ID, other.Generation
	a.machineRequest = func(action string) error { t.Fatalf("unexpected request: %s", action); return nil }
	a.openWorkspaceMenu(1, 1, 0)
	a.st.Workspaces = a.st.Workspaces[1:]
	activateLabel(t, a, "💼 work")
	if a.dialog == nil || other.Section != otherSection || a.oyagoiWorkspace != other.ID {
		t.Fatal("removed workspace affected replacement or no error")
	}
}
