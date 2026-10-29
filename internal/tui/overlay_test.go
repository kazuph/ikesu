package tui

import (
	"encoding/json"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/teststate"
	"io"
	"testing"
	"time"
)

func TestOverlayRealInputAndRestoredLayout(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, "SHELL", "/bin/sh")
	st := model.New(t.TempDir(), 120, 40)
	sup := runtime.NewSupervisor()
	defer sup.KillAll()
	a := NewApp(st, sup, io.Discard, st.ActiveWorkspace().Cwd)
	a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	target := st.FocusedPane()
	before, _ := json.Marshal(st.Snapshot())
	id, err := a.OpenOverlay(target, st.ActiveWorkspace().Cwd, []string{"/bin/cat"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.focusedPane() != id {
		t.Fatal("wrong input target")
	}
	specFeed(a, "overlay-input-proof\n")
	deadline := time.Now().Add(3 * time.Second)
	for !containsScreen(sup.Get(id).ScreenLines(), "overlay-input-proof") {
		if time.Now().After(deadline) {
			t.Fatal("input did not reach overlay")
		}
		time.Sleep(time.Millisecond)
	}
	rect, _ := st.OverlayRect()
	if got := a.rectForPane(id); got.W != func() int { r, _ := model.PaneContentRect(rect, st.PaneBorders); return max(r.W, 1) }() || got.H != func() int { r, _ := model.PaneContentRect(rect, st.PaneBorders); return r.H }() {
		t.Fatal("overlay term geometry", got, rect)
	}
	if !a.popupMouse(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: st.PanesArea().X, Y: st.PanesArea().Y, Button: uv.MouseLeft}}) {
		t.Fatal("mouse passed through overlay")
	}
	a.closeFocusedPane()
	after, _ := json.Marshal(st.Snapshot())
	if string(before) != string(after) {
		t.Fatalf("persistent layout changed: %s -> %s", before, after)
	}
	if a.focusedPane() != target {
		t.Fatal("focus not restored")
	}
}
func containsScreen(lines []string, text string) bool {
	for _, line := range lines {
		if line == text {
			return true
		}
	}
	return false
}
