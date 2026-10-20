package tui

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"io"
	"testing"
)

func TestStage5DelayedRenderedFrameDoesNotFreezeAutomaticTabFollow(t *testing.T) {
	a := specRuntimeApp(t)
	for i := 2; i <= 15; i++ {
		a.st.Apply(model.CmdNewTab{Workspace: 0})
		a.st.ActiveWorkspace().TabRef().Name = fmt.Sprintf("TAB%02d", i)
	}
	a.st.ActiveWorkspace().Tabs[0].Name = "TAB01"
	v := a.NewViewer(io.Discard, 90, 30, nil)
	var old *render.Output
	v.with(a, func(ui *App) {
		ui.st.Apply(model.CmdSelectTab{Workspace: 0, Tab: 5})
		old = render.Compose(render.Input{State: ui.st, ServerUI: true})
		ui.exec(model.CmdSelectTab{Workspace: 0, Tab: 14})
	})
	v.Rendered(a, *old, 0)
	v.with(a, func(ui *App) {
		ui.draw()
		if ui.tabScroll != 120 || ui.tabHits[14].W == 0 {
			t.Fatal("stale frame froze the automatic viewport", ui.tabScroll, ui.tabHits)
		}
	})
}
