package client

import (
	"bytes"
	"os"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/kitty"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/tui"
)

func TestMachineImageOutputWaitsForTerminalStatusAndCoalesces(t *testing.T) {
	out, err := os.CreateTemp(t.TempDir(), "viewer")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	state := model.New(t.TempDir(), 160, 45)
	pane := &render.PaneSnap{Graphics: []kitty.Placement{{Image: kitty.Image{ID: 42, Format: 24, Width: 2, Height: 1, Data: []byte{255, 0, 0, 0, 255, 0}}, X: 6, Y: 4, Cols: 4, Rows: 2}}}
	m := &machineClient{active: "local", order: []string{"local"}, out: out, settings: &localSettings{}, size: tui.Winsize{Cols: 160, Rows: 45}, endpoints: map[string]*managedEndpoint{"local": {snapshot: &render.Input{State: state, Panes: map[int]*render.PaneSnap{state.FocusedPane(): pane}}}}}
	if err := m.paint(); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(out.Name())
	if bytes.Count(first, []byte("\x1b[5n")) != 1 {
		t.Fatal("image frame did not query terminal completion")
	}
	if ansi.Strip(string(first)) == "" {
		t.Fatal("initial frame did not render real text cells")
	}
	for _, red := range []byte{1, 2, 3} {
		pane.Graphics[0].Image.Data = []byte{red, 0, 0, 0, 255, 0}
		if err := m.paint(); err != nil {
			t.Fatal(err)
		}
	}
	waiting, _ := os.ReadFile(out.Name())
	if !bytes.Equal(waiting, first) {
		t.Fatal("image output advanced before terminal completion")
	}
	// There is no endpoint connection: forwarding this reply would fail.
	if err := m.input(input.Event{Kind: input.KindUnknown, Raw: []byte("\x1b[0n")}); err != nil {
		t.Fatalf("terminal status leaked to the pane: %v", err)
	}
	after, _ := os.ReadFile(out.Name())
	changed := after[len(first):]
	if bytes.Count(changed, []byte("a=t,")) != 1 || !bytes.Contains(changed, []byte("AwAAAP8A")) || bytes.Count(changed, []byte("\x1b[5n")) != 1 {
		t.Fatal("terminal completion did not release only the latest image")
	}
	if text := ansi.Strip(string(changed)); text != "" {
		t.Fatalf("image-only deferred output rewrote unchanged text: %q", text)
	}
	if bytes.Contains(changed, []byte("\x1b[2J")) {
		t.Fatal("image-only deferred output cleared the screen")
	}
}
