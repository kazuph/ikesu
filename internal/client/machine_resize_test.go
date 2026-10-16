package client

import (
	"bytes"
	"os"
	"testing"

	"github.com/kazuph/goterrace/internal/kitty"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/tui"
)

func TestPendingResizeKeepsSnapshotGraphics(t *testing.T) {
	out, err := os.CreateTemp(t.TempDir(), "viewer")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	state := model.New(t.TempDir(), 160, 45)
	pane := state.FocusedPane()
	snapshot := &render.Input{State: state, Panes: map[int]*render.PaneSnap{pane: {Graphics: []kitty.Placement{{Image: kitty.Image{ID: 42, Format: 24, Width: 2, Height: 1, Data: []byte{255, 0, 0, 0, 255, 0}}, X: 6, Y: 4, Cols: 4, Rows: 2}}}}}
	m := &machineClient{active: "local", order: []string{"local"}, out: out, settings: &localSettings{}, size: tui.Winsize{Cols: 160, Rows: 45}, endpoints: map[string]*managedEndpoint{"local": {snapshot: snapshot}}}
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	before, _ := out.Seek(0, 1)
	first, err := os.ReadFile(out.Name())
	if err != nil || !bytes.Contains(first, []byte("a=t,")) {
		t.Fatalf("fixture did not display an image: %v", err)
	}
	m.size = tui.Winsize{Cols: 140, Rows: 40}
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	resized := data[before:]
	if bytes.Contains(resized, []byte("a=d,")) || bytes.Contains(resized, []byte("\x1b[2J")) || !bytes.Contains(resized, []byte("a=p,")) {
		t.Fatal("waiting for the resized snapshot erased the displayed image")
	}
}
