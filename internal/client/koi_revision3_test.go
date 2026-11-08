package client

import (
	"github.com/kazuph/ikesu/internal/machine"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"

	"github.com/kazuph/ikesu/internal/tui"
	"os"
	"testing"
	"time"
)

func koiRev3Client(t *testing.T) (*machineClient, *os.File) {
	t.Helper()
	state := model.New(t.TempDir(), 160, 45)
	state.SidebarVisible = true
	state.SidebarSize = 26
	state.RightSidebarSize = 30
	snapshot := render.Input{State: state, Panes: map[int]*render.PaneSnap{1: {Agent: "codex", Status: render.StatusGlyph{Key: "idle"}}}}
	file, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	m := &machineClient{active: "local", order: []string{"local"}, out: file, koiKitty: true, size: tui.Winsize{Cols: 160, Rows: 45, Xpixel: 1280, Ypixel: 720}, settings: &localSettings{}, endpoints: map[string]*managedEndpoint{"local": {snapshot: &snapshot, profile: machine.Profile{ID: "local", Enabled: true}}}}
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	// Entry begins outside; the next tick delivers its first visible crop.
	timer := time.NewTimer(render.KoiInterval)
	<-timer.C
	if err := m.paintKoi(time.Now()); err != nil {
		t.Fatal(err)
	}
	return m, file
}
