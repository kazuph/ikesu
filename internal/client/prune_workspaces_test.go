package client

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/render"
)

func TestPruneAcrossTwoServersWithLegacyPeer(t *testing.T) {
	h := newRightMachineHarnessPeers(t, nil, []string{"local", "remote", "legacy"}, true)
	m := h.m
	paths := map[string]string{}
	for _, id := range m.order {
		root := t.TempDir()
		runPruneGit(t, root, "init", "-qb", "main")
		runPruneGit(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "init")
		if err := os.WriteFile(filepath.Join(root, ".git", "info", "exclude"), []byte("payload\n"), 0600); err != nil {
			t.Fatal(err)
		}
		branches := []string{id + "-merged"}
		if id == "remote" {
			branches = append(branches, "remote-keep")
		}
		checkouts := t.TempDir()
		for _, branch := range branches {
			path := filepath.Join(checkouts, branch)
			paths[branch] = path
			runPruneGit(t, root, "worktree", "add", "-qb", branch, path)
			if err := os.WriteFile(filepath.Join(path, "payload"), []byte(strings.Repeat("x", 2_000_000)), 0600); err != nil {
				t.Fatal(err)
			}
			h.api(id, api.MethodWorktreeOpen, api.WorktreeOpenParams{Cwd: &root, Path: &path, Label: &branch})
		}
	}
	h.until("opened worktrees", func() bool {
		return len(m.endpoints["local"].snapshot.State.Workspaces) == 3 &&
			len(m.endpoints["remote"].snapshot.State.Workspaces) == 4 &&
			len(m.endpoints["legacy"].snapshot.State.Workspaces) == 3
	})

	key := func(code rune) {
		t.Helper()
		if err := m.input(input.Event{Kind: input.KindKey, Key: uv.Key{Code: code}}); err != nil {
			t.Fatal(err)
		}
	}
	mouseRelease := func(x, y int) {
		t.Helper()
		if err := m.input(input.Event{Kind: input.KindMouseRelease, Mouse: uv.Mouse{Button: uv.MouseLeft, X: x, Y: y}}); err != nil {
			t.Fatal(err)
		}
	}
	openMenu := func() {
		t.Helper()
		r := render.SidebarMenuRect(m.endpoints["local"].snapshot.State, true)
		if err := m.sendInput("local", fmt.Appendf(nil, "\x1b[<0;%d;%dM\x1b[<0;%d;%dm", r.X+1, r.Y+1, r.X+1, r.Y+1)); err != nil {
			t.Fatal(err)
		}
		h.until("sidebar main menu", func() bool { return m.endpoints["local"].snapshot.Menu != nil })
		menu := m.endpoints["local"].snapshot.Menu
		for i, item := range menu.Items {
			if item.Label != "prune merged workspaces…" {
				continue
			}
			outer := render.MenuRect(menu, m.size.Cols, m.size.Rows)
			x, y := outer.X+2, outer.Y+2+i-render.MenuScroll(menu, m.size.Rows)
			if err := m.sendInput("local", fmt.Appendf(nil, "\x1b[<0;%d;%dM\x1b[<0;%d;%dm", x, y, x, y)); err != nil {
				t.Fatal(err)
			}
			h.until("prune dialog from machine_menu", func() bool { return m.prune != nil })
			return
		}
		t.Fatal("prune menu item missing")
	}
	preview := func() *render.Dialog { return m.prune.dialog.Render(m.pruneArea()) }

	openMenu()
	if lines := strings.Join(preview().Lines, "\n"); !strings.Contains(lines, "● 接続中の全マシン (local, remote, legacy)") {
		t.Fatalf("scope dialog: %s", lines)
	}
	key(uv.KeyTab)
	key(uv.KeyEnter)
	h.until("local-only preview", func() bool { return m.prune.request > 0 && len(m.prune.pending) == 0 })
	if sel := m.prune.dialog.Selection(); len(sel) != 1 || sel[0].Machine != "local" {
		t.Fatalf("local-only selection: %+v", sel)
	}
	key(uv.KeyEscape)

	openMenu()
	_, _, scan := render.DialogRects(preview(), m.pruneArea())
	mouseRelease(scan.X, scan.Y)
	h.until("all-machines preview", func() bool { return m.prune.request > 0 && len(m.prune.pending) == 0 })
	box := preview()
	if !strings.Contains(box.Detail, "reclaimable:") || !strings.Contains(box.Detail, "3 workspaces") ||
		!strings.Contains(strings.Join(box.Lines, "\n"), "legacy · unsupported") {
		t.Fatalf("all-machines preview: %+v", box)
	}
	key('a')
	if n := len(m.prune.dialog.Selection()); n != 0 {
		t.Fatalf("toggle all off: %d", n)
	}
	key('a')
	for i, line := range box.Lines {
		if strings.Contains(line, "remote-keep") {
			outer, _, _ := render.DialogRects(box, m.pruneArea())
			mouseRelease(outer.X+4, outer.Y+3+i)
			break
		}
	}
	sel := m.prune.dialog.Selection()
	if len(sel) != 2 || sel[0].Machine != "local" || sel[1].Candidate.Revision.Branch != "remote-merged" {
		t.Fatalf("individual toggle: %+v", sel)
	}
	wantSize := fmt.Sprintf("%.2f MB", float64(sel[0].Candidate.Bytes+sel[1].Candidate.Bytes)/1_000_000)
	key(uv.KeyEnter)
	if !strings.Contains(preview().Detail, "Delete 2 workspaces") {
		t.Fatalf("confirmation: %+v", preview())
	}
	key(uv.KeyEnter)
	h.until("prune execution summary", func() bool {
		return strings.Contains(preview().Detail, "2 件削除") &&
			len(m.endpoints["local"].snapshot.State.Workspaces) == 2 &&
			len(m.endpoints["remote"].snapshot.State.Workspaces) == 3
	})
	if !strings.Contains(preview().Detail, wantSize) || !strings.Contains(preview().Lines[0], "skipped 0 · failed 0 · unknown 0") {
		t.Fatalf("result: %+v", preview())
	}
	for branch, path := range paths {
		_, err := os.Stat(path)
		switch branch {
		case "local-merged", "remote-merged":
			if !os.IsNotExist(err) {
				t.Fatalf("deleted checkout %q: %v", branch, err)
			}
		default:
			if err != nil {
				t.Fatalf("protected checkout %q: %v", branch, err)
			}
		}
	}
}

func runPruneGit(t *testing.T, root string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}
