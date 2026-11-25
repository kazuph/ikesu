package tui

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/worktree"
)

func TestStandalonePruneMenuAndTOCTOU(t *testing.T) {
	a, root := worktreeApp(t)
	pruneTestGit(t, root, "branch", "-M", "main")
	path := filepath.Join(t.TempDir(), "feature")
	pruneTestGit(t, root, "worktree", "add", "-qb", "feature", path)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	label := "Merged feature"
	opened, err := executeTestWorktree(t, a, WorktreeRequest{Operation: "open", Cwd: root, Path: &path, Label: &label})
	if err != nil {
		t.Fatal(err)
	}
	paneID := a.st.Workspaces[opened.Outcome.Index].TabRef().Layout.Focused()
	area := layout.Rect{W: a.st.Cols, H: a.st.Rows}
	key := func(code rune) { a.handleKey(uv.Key{Code: code}, nil) }
	openAndScan := func() {
		t.Helper()
		a.openMainMenu(0, 0)
		activateLabel(t, a, menuPruneWorkspaces)
		key(uv.KeyEnter)
		waitWorktreeUI(t, a, func() bool { return a.prune.stage == prunePreview })
		box := a.prune.Render(area)
		if len(a.prune.Selection()) != 1 || !strings.Contains(box.Detail, "reclaimable:") || !strings.Contains(strings.Join(box.Lines, "\n"), "merged into main") {
			t.Fatalf("preview: %+v", box)
		}
	}
	openAndScan()
	if err := os.WriteFile(filepath.Join(path, "draft"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	key(uv.KeyEnter)
	key(uv.KeyEnter)
	waitWorktreeUI(t, a, func() bool { return a.prune.stage == pruneSummary })
	if a.prune.skipped != 1 || !strings.Contains(strings.Join(a.prune.outcomes, "\n"), "dirty") || len(a.st.Workspaces) != 2 || a.sup.Get(paneID) == nil {
		t.Fatalf("dirty checkout closed: %+v", a.prune)
	}
	if got, err := os.ReadFile(filepath.Join(path, "draft")); err != nil || string(got) != "keep" {
		t.Fatalf("dirty file: %q, %v", got, err)
	}
	key(uv.KeyEscape)
	if err := os.Remove(filepath.Join(path, "draft")); err != nil {
		t.Fatal(err)
	}
	openAndScan()
	a.SetReported(paneID, "codex", "working")
	a.SetReported(paneID, "codex", "idle")
	key(uv.KeyEnter)
	key(uv.KeyEnter)
	waitWorktreeUI(t, a, func() bool { return a.prune.stage == pruneSummary })
	if a.prune.skipped != 1 || len(a.st.Workspaces) != 2 {
		t.Fatalf("workspace with post-scan agent activity closed: %+v", a.prune)
	}
	key(uv.KeyEscape)
	openAndScan()
	key(uv.KeyEnter)
	key(uv.KeyEnter)
	waitWorktreeUI(t, a, func() bool { return a.prune.stage == pruneSummary })
	if a.prune.deleted != 1 || a.prune.bytes == 0 || len(a.st.Workspaces) != 1 || a.sup.Get(paneID) != nil {
		t.Fatalf("deletion result: %+v", a.prune)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("checkout still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		t.Fatalf("main checkout removed: %v", err)
	}
	pruneTestGit(t, root, "show-ref", "--verify", "refs/heads/feature")
}

func TestPruneProtectsWorkspaceAndPaneState(t *testing.T) {
	a, root := worktreeApp(t)
	pruneTestGit(t, root, "branch", "-M", "main")
	path := filepath.Join(t.TempDir(), "feature")
	pruneTestGit(t, root, "worktree", "add", "-qb", "feature", path)
	opened, err := executeTestWorktree(t, a, WorktreeRequest{Operation: "open", Cwd: root, Path: &path})
	if err != nil {
		t.Fatal(err)
	}
	ws := a.st.Workspaces[opened.Outcome.Index]
	pid := ws.TabRef().Layout.Focused()
	viewer := a.NewViewer(io.Discard, a.st.Cols, a.st.Rows, nil)
	viewers := []*Viewer{viewer}
	expect := func(w *model.Workspace, want string) {
		t.Helper()
		if _, got := a.pruneTarget(w, viewers, a.pruneCwds()); got != want {
			t.Fatalf("protection = %q, want %q", got, want)
		}
	}
	a.st.Workspaces[0].Worktree = &api.WorkspaceWorktreeInfo{RepoRoot: root, CheckoutPath: root}
	expect(a.st.Workspaces[0], "not a linked worktree")
	viewer.SetOyagoi(a, ws.ID, ws.Generation)
	expect(ws, "HQ")
	viewer.SetOyagoi(a, 0, "")
	viewer.SelectWorkspace(a, ws.ID)
	expect(ws, "focused")
	viewer.SelectWorkspace(a, a.st.Workspaces[0].ID)
	for _, state := range []string{"working", "blocked"} {
		a.SetReported(pid, "codex", state)
		expect(ws, "active agent")
	}
	a.SetReported(pid, "codex", "idle")
	expect(ws, "")
	initial, _ := a.pruneTarget(ws, viewers, a.pruneCwds())
	a.SetReported(pid, "codex", "working")
	a.SetReported(pid, "codex", "idle")
	after, _ := a.pruneTarget(ws, viewers, a.pruneCwds())
	if initial.candidate.PaneState == after.candidate.PaneState {
		t.Fatal("agent activity after preview did not change the prune identity")
	}

	old := a.st.Workspaces
	a.st.Workspaces = []*model.Workspace{ws}
	expect(ws, "last workspace")
	a.st.Workspaces = old
	request := endpoint.PruneExecRequest{ServerGeneration: "old", Candidates: []endpoint.PruneCandidate{after.candidate}}
	out := a.PruneExec(context.Background(), "new", request, noPruneViewers, func() bool { return true })
	if len(out.Outcomes) != 1 || out.Outcomes[0].State != endpoint.PruneSkipped || out.Outcomes[0].Reason != "server changed" {
		t.Fatalf("old generation accepted: %+v", out)
	}

	src, err := worktree.Source(path)
	if err != nil {
		t.Fatal(err)
	}
	a.st.Workspaces[0].Worktree = worktree.Metadata(src, path)
	expect(ws, "checkout used by another workspace")
}

func pruneTestGit(t *testing.T, cwd string, args ...string) {
	t.Helper()
	if output, err := exec.Command("git", append([]string{"-C", cwd}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func TestPruneSizeFormatting(t *testing.T) {
	for _, tc := range []struct {
		bytes uint64
		want  string
	}{{0, "0 B"}, {1_024, "1.02 KB"}, {1_420_000, "1.42 MB"}, {6_840_000_000, "6.84 GB"}} {
		if got := pruneSize(tc.bytes); got != tc.want {
			t.Errorf("%d = %s, want %s", tc.bytes, got, tc.want)
		}
	}
}
