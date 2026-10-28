package client

import (
	"bytes"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/input"
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
	if err := m.paintKoi(time.Now().Add(render.KoiInterval)); err != nil {
		t.Fatal(err)
	}
	return m, file
}
func TestKoiRev3FirstRippleLatencyAndFenceDeferral(t *testing.T) {
	for _, waiting := range []bool{false, true} {
		t.Run(map[bool]string{false: "ready", true: "existing-fence-timeout"}[waiting], func(t *testing.T) {
			m, file := koiRev3Client(t)
			if !waiting {
				m.outputFence.Accept([]byte("\x1b[0n"))
			}
			startOffset, _ := file.Seek(0, 1)
			r := m.koiScene.Pond
			clicked := time.Now()
			if err := m.input(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: r.X + r.W/2, Y: r.Y + r.H/2, Button: uv.MouseLeft}}); err != nil {
				t.Fatal(err)
			}
			if waiting {
				current, _ := file.Seek(0, 1)
				if current != startOffset {
					t.Fatal("existing fence was bypassed")
				}
				timer := time.NewTimer(max(time.Duration(0), time.Until(m.outputFence.Deadline())))
				defer timer.Stop()
				<-timer.C
				if err := m.paint(); err != nil {
					t.Fatal(err)
				}
			}
			wire, err := os.ReadFile(file.Name())
			if err != nil {
				t.Fatal(err)
			}
			elapsed := time.Since(clicked)
			if !bytes.Contains(wire[startOffset:], []byte("z=400")) {
				t.Fatal("first writable frame has no ripple")
			}
			limit := 50 * time.Millisecond
			if waiting {
				limit += render.TerminalStatusWait
			}
			if elapsed > limit {
				t.Fatalf("first ripple delay %v exceeds %v", elapsed, limit)
			}
			t.Logf("waiting=%t click_to_first_ripple=%s", waiting, elapsed)
		})
	}
}
