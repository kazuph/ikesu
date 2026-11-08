package tui

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/teststate"
	"github.com/kazuph/ikesu/internal/worktree"
)

// A reused space keeps the worktree metadata of where it was created; once its
// first pane's shell lives in another checkout the space must follow it.
func TestStaleWorktreeMetadataFollowsTheFirstPaneShell(t *testing.T) {
	teststate.Lock(t)
	root := t.TempDir()
	old := &api.WorkspaceWorktreeInfo{RepoKey: filepath.Join(root, "a/.git"), RepoName: "a", RepoRoot: filepath.Join(root, "a"), CheckoutPath: filepath.Join(root, "a/.worktree/fix"), IsLinkedWorktree: true}
	other := api.WorktreeSourceInfo{RepoKey: filepath.Join(root, "b/.git"), RepoName: "b", RepoRoot: filepath.Join(root, "b"), SourceCheckoutPath: filepath.Join(root, "b")}
	for _, c := range []struct {
		name    string
		nameCwd string
		entry   gitCwdActivity
		want    *api.WorkspaceWorktreeInfo
	}{
		{"moved to another repo", filepath.Join(root, "b/src"), gitCwdActivity{loaded: true, source: other}, &api.WorkspaceWorktreeInfo{RepoKey: other.RepoKey, RepoName: "b", RepoRoot: other.RepoRoot, CheckoutPath: other.RepoRoot}},
		{"still inside the checkout", filepath.Join(root, "a/.worktree/fix/sub"), gitCwdActivity{loaded: true, source: other}, old},
		{"left git", filepath.Join(root, "plain"), gitCwdActivity{loaded: true, sourceErr: errors.New("not a git repository")}, nil},
		{"not read yet", filepath.Join(root, "b"), gitCwdActivity{source: other}, old},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := specRuntimeApp(t)
			copied := *old
			a.st.Workspaces[0].Worktree = &copied
			id := a.st.Workspaces[0].ID
			a.refreshStaleWorktrees([]workspaceGitTarget{{id, old.CheckoutPath, c.nameCwd}}, map[string]gitCwdActivity{c.nameCwd: c.entry})
			got := a.st.Workspaces[0].Worktree
			if (got == nil) != (c.want == nil) || got != nil && *got != *c.want {
				t.Fatalf("worktree=%+v want %+v", got, c.want)
			}
			moved := got != old && (got == nil || *got != *old)
			if wantCwd := map[bool]string{true: c.nameCwd, false: a.st.Workspaces[0].Cwd}[moved]; a.st.Workspaces[0].Cwd != wantCwd {
				t.Fatalf("cwd=%q want %q", a.st.Workspaces[0].Cwd, wantCwd)
			}
		})
	}
}

// The git line reads the space's directory, so it follows the re-anchor, and
// coming back to the first checkout re-anchors again.
func TestStaleWorktreeRoundTripMovesTheSpaceDirectory(t *testing.T) {
	teststate.Lock(t)
	root := t.TempDir()
	a := specRuntimeApp(t)
	srcA := api.WorktreeSourceInfo{RepoKey: filepath.Join(root, "a/.git"), RepoName: "a", RepoRoot: filepath.Join(root, "a"), SourceCheckoutPath: filepath.Join(root, "a/.worktree/fix")}
	srcB := api.WorktreeSourceInfo{RepoKey: filepath.Join(root, "b/.git"), RepoName: "b", RepoRoot: filepath.Join(root, "b"), SourceCheckoutPath: filepath.Join(root, "b")}
	a.st.Workspaces[0].Cwd = srcA.SourceCheckoutPath
	a.st.Workspaces[0].Worktree = worktree.Metadata(srcA, srcA.SourceCheckoutPath)
	id := a.st.Workspaces[0].ID
	for _, step := range []api.WorktreeSourceInfo{srcB, srcA} {
		a.refreshStaleWorktrees([]workspaceGitTarget{{id, a.st.Workspaces[0].Cwd, step.SourceCheckoutPath}}, map[string]gitCwdActivity{step.SourceCheckoutPath: {loaded: true, source: step}})
		ws := a.st.Workspaces[0]
		if ws.Cwd != step.SourceCheckoutPath || ws.Worktree == nil || ws.Worktree.RepoKey != step.RepoKey || ws.Worktree.CheckoutPath != step.SourceCheckoutPath {
			t.Fatalf("after moving to %s: cwd=%q worktree=%+v", step.RepoName, ws.Cwd, ws.Worktree)
		}
	}
}
