package tui

import (
	"bytes"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/runtime"
	"testing"
)

func TestLeftSidebarDividerBothColumnsStillDrag(t *testing.T) {
	for _, offset := range []int{-1, 0} {
		a := NewApp(model.New(t.TempDir(), 160, 30), runtime.NewSupervisor(), &bytes.Buffer{}, "")
		x := a.st.SidebarArea().W + offset
		a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: x, Y: 5, Button: uv.MouseLeft}})
		if a.sidebarGesture == nil || !a.sidebarGesture.divider {
			t.Fatal("divider edge not grabbed", offset)
		}
		a.onMouse(input.Event{Kind: input.KindMouseMotion, Mouse: uv.Mouse{X: 33, Y: 5, Button: uv.MouseLeft}})
		a.onMouse(input.Event{Kind: input.KindMouseRelease, Mouse: uv.Mouse{X: 33, Y: 5, Button: uv.MouseLeft}})
		if a.st.SidebarArea().W != 34 || !a.st.SidebarManual {
			t.Fatal("divider drag not applied", offset)
		}
	}
}
