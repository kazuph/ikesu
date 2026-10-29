package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/teststate"
	"testing"
)

func TestSidebarFullscreenCopyActualPTYButtons(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	pid := a.focusedPane()
	inner := a.innerRect(pid)
	p, err := a.sup.SpawnWith(pid, a.cwd, inner.W, inner.H, nil, []string{"/bin/sh", "-c", "printf '│項目A\\r\\n│項目B\\r\\n│READY\\r\\n'; exec sleep 60"})
	if err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "│READY")
	for i := 0; i < 2; i++ {
		a.draw()
		r := a.actionBar.Copy
		if r.W == 0 {
			t.Fatal("COPY button missing")
		}
		m := uv.Mouse{X: r.X, Y: r.Y, Button: uv.MouseLeft}
		a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: m})
		a.onMouse(input.Event{Kind: input.KindMouseRelease, Mouse: m})
	}
	if a.cm == nil || !a.cm.fullscreen {
		t.Fatal("COPY twice did not enter fullscreen")
	}
	var out *render.Output
	a.snapshotSink = func(in render.Input) error { out = render.Compose(in); return nil }
	a.draw()
	area := a.st.PanesArea()
	for y := 0; y < 3; y++ {
		if c := out.Frame.Cell(area.X, area.Y+y); c.Content != "│" {
			t.Fatal("real PTY body line erased", y, c)
		}
	}
}
