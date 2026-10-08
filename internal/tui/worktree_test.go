package tui

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/runtime"
)

func worktreeApp(t *testing.T) (*App, string) {
	t.Helper()
	r := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELL", "/bin/sh")
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "init"}, {"branch", "existing"}} {
		if b, e := exec.Command("git", append([]string{"-C", r}, args...)...).CombinedOutput(); e != nil {
			t.Fatalf("%v %s", e, b)
		}
	}
	sup := runtime.NewSupervisor()
	a := NewApp(model.New(r, 120, 35), sup, io.Discard, r)
	t.Cleanup(func() {
		for _, w := range a.st.Workspaces {
			for _, tab := range w.Tabs {
				for _, p := range tab.Layout.PaneIDs() {
					sup.Kill(p)
				}
			}
		}
	})
	return a, r
}
func activateLabel(t *testing.T, a *App, label string) {
	t.Helper()
	for i, item := range a.menu.m.Items {
		if item.Label == label {
			a.activateMenu(i)
			return
		}
	}
	t.Fatalf("menu missing %q", label)
}
func TestSidebarWorktreeNewExistingOpenDelete(t *testing.T) {
	a, r := worktreeApp(t)
	a.openWorkspaceMenu(2, 3, 0)
	activateLabel(t, a, "New worktree")
	*a.dialog.input = "feat/sidebar"
	d := a.dialog
	a.dialog = nil
	d.run(a)
	if len(a.st.Workspaces) != 2 || a.st.Active != 1 {
		t.Fatalf("create: %d active=%d", len(a.st.Workspaces), a.st.Active)
	}
	path := a.st.Workspaces[1].Cwd
	if _, e := os.Stat(filepath.Join(path, ".git")); e != nil {
		t.Fatal(e)
	}
	if a.st.Workspaces[0].Worktree == nil || a.st.Workspaces[1].Worktree.RepoKey != a.st.Workspaces[0].Worktree.RepoKey {
		t.Fatal("group metadata")
	}
	a.openWorkspaceMenu(2, 3, 0)
	activateLabel(t, a, "New worktree")
	*a.dialog.input = "existing"
	d = a.dialog
	a.dialog = nil
	d.run(a)
	if len(a.st.Workspaces) != 3 {
		t.Fatal("existing checkout missing")
	}
	a.openWorkspaceMenu(2, 3, 0)
	activateLabel(t, a, "Open worktree...")
	activateLabel(t, a, "feat/sidebar")
	if a.st.Active != 1 || len(a.st.Workspaces) != 3 {
		t.Fatal("open existing duplicated checkout")
	}
	a.openWorkspaceMenu(2, 3, 0)
	activateLabel(t, a, "Collapse")
	if len(render.SidebarRows(a.st, nil)) != 1 {
		t.Fatal("collapsed group shows children")
	}
	a.openWorkspaceMenu(2, 3, 0)
	activateLabel(t, a, "Expand")
	a.openWorkspaceMenu(2, 3, 1)
	activateLabel(t, a, "Delete worktree checkout...")
	a.dialog = nil // Cancellation must leave Git and the workspace untouched.
	if _, e := os.Stat(path); e != nil || len(a.st.Workspaces) != 3 {
		t.Fatal("cancel removed checkout")
	}
	a.deleteWorktreeDialog(1, false)
	d = a.dialog
	a.dialog = nil
	d.run(a)
	if len(a.st.Workspaces) != 2 || a.st.ActiveWorkspace() == nil {
		t.Fatal("delete focus invalid")
	}
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("delete did not remove checkout")
	}
	src, e := a.WorktreeSource(r, nil)
	if e != nil {
		t.Fatal(e)
	}
	list, e := a.WorktreeList(src)
	if e != nil || len(list) != 2 {
		t.Fatalf("%+v %v", list, e)
	}
}
func TestWorktreeDirtyFailureKeepsFocusAndPersistedGroup(t *testing.T) {
	a, _ := worktreeApp(t)
	branch := "dirty"
	id := "s1"
	out, e := a.CreateWorktree(api.WorktreeCreateParams{WorkspaceID: &id, Branch: &branch, Focus: true})
	if e != nil {
		t.Fatal(e)
	}
	path := out.Worktree.Path
	if e := os.WriteFile(filepath.Join(path, "unsaved"), []byte("keep"), 0600); e != nil {
		t.Fatal(e)
	}
	a.deleteWorktreeDialog(out.Index, false)
	d := a.dialog
	a.dialog = nil
	d.run(a)
	if a.dialog == nil || a.dialog.d.Title != "Worktree operation failed" || len(a.st.Workspaces) != 2 || a.st.Active != out.Index {
		t.Fatal("dirty failure mutated workspace/focus")
	}
	restored := model.Restore(a.st.Snapshot())
	if restored.Workspaces[1].Worktree == nil || restored.Workspaces[1].Worktree.CheckoutPath != path {
		t.Fatal("restore lost group metadata")
	}
	if _, e := a.RemoveWorktree(out.Index, true); e != nil {
		t.Fatal(e)
	}
	if a.st.ActiveWorkspace() == nil || a.st.Active != 0 {
		t.Fatal("active deletion lost focus")
	}
}

func TestWorktreeMoveBackToGroupAndFailureWithoutMutation(t *testing.T) {
	a, r := worktreeApp(t)
	branch := "move"
	out, e := a.CreateWorktree(api.WorktreeCreateParams{Cwd: &r, Branch: &branch, Focus: true})
	if e != nil {
		t.Fatal(e)
	}
	id := a.st.Workspaces[out.Index].ID
	a.exec(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
	a.exec(model.CmdMoveWorkspace{Index: out.Index, InsertIndex: 0})
	a.openWorkspaceMenu(2, 3, 0)
	activateLabel(t, a, "Move to worktree group")
	if a.st.Workspaces[1].ID != id || a.st.Active != 1 {
		t.Fatal("group movement lost child/focus")
	}
	before := len(a.st.Workspaces)
	invalid := "not a branch"
	if _, e := a.CreateWorktree(api.WorktreeCreateParams{Cwd: &r, Branch: &invalid}); e == nil {
		t.Fatal("invalid branch accepted")
	}
	if len(a.st.Workspaces) != before || a.st.Active != 1 {
		t.Fatal("create failure mutated app")
	}
	unknown := "unregistered"
	if _, e := a.OpenWorktree(api.WorktreeOpenParams{Cwd: &r, Branch: &unknown}); e == nil {
		t.Fatal("unknown checkout silently substituted")
	}
}
