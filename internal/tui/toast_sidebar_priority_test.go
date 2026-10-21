package tui

import (
	"bytes"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/runtime"
)

func TestToastClickPrecedesRightSidebar(t *testing.T) {
	for _, mode := range []string{"", "compact", "hidden"} {
		t.Run("mode="+mode, func(t *testing.T) {
			st := model.New(t.TempDir(), 160, 45)
			st.RightSidebarMode = mode
			first := st.FocusedPane()
			st.Apply(model.CmdNewWorkspace{Cwd: t.TempDir(), NoFocus: true})
			target := st.Workspaces[1].Tabs[0].Layout.Focused()
			a := NewApp(st, runtime.NewSupervisor(), &bytes.Buffer{}, "")
			a.ShowToast("finished", "target", "info", target)
			a.draw()
			r := a.toastRect
			x, y := r.X+r.W-2, r.Y+1
			if r.W == 0 || !pointIn(x, y, r) {
				t.Fatal("toast not rendered", r)
			}
			if mode != "hidden" && !pointIn(x, y, st.RightSidebarArea()) {
				t.Fatal("test must overlap sidebar", r, st.RightSidebarArea())
			}
			a.click(uv.Mouse{X: x, Y: y, Button: uv.MouseRight})
			if a.toast == nil || a.menu == nil || a.menu.pane != first || st.FocusedPane() != first {
				t.Fatal("toast right click must retain notification and open current pane menu without sidebar navigation")
			}
			a.menu = nil
			a.click(uv.Mouse{X: x, Y: y, Button: uv.MouseLeft})
			if a.toast != nil || st.FocusedPane() != target {
				t.Fatal("toast left click must dismiss and navigate to its target")
			}
		})
	}
}
