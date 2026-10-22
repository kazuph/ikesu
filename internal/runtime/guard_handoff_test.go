package runtime

import (
	"fmt"
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
