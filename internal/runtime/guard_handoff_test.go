package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/kazuph/ikesu/internal/guard"
)

func TestHandoffRebindsEveryGuardMarker(t *testing.T) {
	s := NewSupervisor()
	root := t.TempDir()
	socket := filepath.Join(root, "live.sock")
	for id := 1; id <= 2; id++ {
		a := guard.NewAssets(root, fmt.Sprintf("p_%d", id), "/usr/bin/true", guard.Config{})
		_, e := a.Prepare([]string{"HOME=" + root, "PATH=/bin", fmt.Sprintf("IKESU_PANE_ID=p%d", id), "IKESU_SOCKET_PATH=" + root + "/stale.sock"}, root)
		if e != nil {
			t.Fatal(e)
		}
		if e = a.Sync("codex"); e != nil {
			t.Fatal(e)
		}
		s.panes[id] = &Pane{ID: id, guard: guard.Import(a.Transfer())}
	}
	if e := s.RebindGuardSocket(socket); e != nil {
		t.Fatal(e)
	}
	for id, p := range s.panes {
		meta, active, e := guard.ReadMarker(guard.Input{Env: map[string]string{"IKESU_GUARD_MARKER": p.guard.MarkerPath, "IKESU_PANE_ID": fmt.Sprintf("p%d", id), "IKESU_SOCKET_PATH": root + "/stale.sock"}})
		if e != nil || !active || meta.Socket != socket {
			t.Fatal(meta, active, e)
		}
	}
}

func TestHandoffDoesNotInjectDisabledGuard(t *testing.T) {
	s := NewSupervisor()
	root := t.TempDir()
	disabled := false
	a := guard.NewAssets(root, "p_1", "/usr/bin/true", guard.Config{Enabled: &disabled})
	if _, err := a.Prepare([]string{"IKESU_PANE_ID=p1", "PATH=/bin"}, root); err != nil {
		t.Fatal(err)
	}
	s.panes[1] = &Pane{ID: 1, guard: guard.Import(a.Transfer())}
	if err := s.RebindGuardSocket(filepath.Join(root, "next.sock")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(root, "guard")); !os.IsNotExist(err) {
		t.Fatalf("disabled pane gained assets: %v", err)
	}
}

func TestHandoffRestoresEarlierPanesWhenLaterGenerationFails(t *testing.T) {
	s := NewSupervisor()
	root := t.TempDir()
	old := map[int]string{}
	for id := 1; id <= 2; id++ {
		a := guard.NewAssets(root, fmt.Sprintf("p_%d", id), "/usr/bin/true", guard.Config{})
		if _, err := a.Prepare([]string{fmt.Sprintf("IKESU_PANE_ID=p%d", id), "PATH=/usr/bin:/bin", "IKESU_SOCKET_PATH=" + root + "/old.sock"}, root); err != nil {
			t.Fatal(err)
		}
		if err := a.Sync("codex"); err != nil {
			t.Fatal(err)
		}
		name, err := os.Readlink(filepath.Join(root, "guard", "shell", fmt.Sprintf("p_%d", id), "current"))
		if err != nil {
			t.Fatal(err)
		}
		old[id] = name
		s.panes[id] = &Pane{ID: id, guard: guard.Import(a.Transfer())}
	}
	bad := filepath.Join(root, "guard", "shell", "p_2", "bash-env.current")
	if err := os.Remove(bad); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(bad, 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.RebindGuardSocket(filepath.Join(root, "next.sock")); err == nil {
		t.Fatal("later pane publication unexpectedly succeeded")
	}
	for id, p := range s.panes {
		current, err := os.Readlink(filepath.Join(root, "guard", "shell", fmt.Sprintf("p_%d", id), "current"))
		if err != nil || current != old[id] {
			t.Fatalf("pane %d retained new generation %s: %v", id, current, err)
		}
		state := p.guard.Transfer()
		if state.Executable != "/usr/bin/true" || state.Meta.Socket != root+"/old.sock" {
			t.Fatalf("pane %d retained new binding: %+v", id, state)
		}
	}
}
