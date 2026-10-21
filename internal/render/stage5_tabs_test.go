package render

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/model"
	"testing"
)

func TestStage5TabCenterAndMouseControls(t *testing.T) {
	for _, mouse := range []bool{true, false} {
		s := testState(90, 30)
		ws := s.ActiveWorkspace()
		ws.TabRef().Name = "TAB01"
		for i := 2; i <= 15; i++ {
			s.Apply(model.CmdNewTab{Workspace: 0})
			ws.TabRef().Name = fmt.Sprintf("TAB%02d", i)
		}
		offset := 0
		for i := 0; i < 15; i++ {
			ws.ActiveTab = i
			in := Input{State: s, ServerUI: true, TabScroll: offset}
			in.SidebarConfig.UI.MouseCapture = &mouse
			out := Compose(in)
			offset = out.TabScroll
			want := max(0, i-2) * 12
			limit := 120
			if !mouse {
				limit = 108
			}
			want = min(want, limit)
			if offset != want {
				t.Fatalf("mouse=%v tab=%d offset=%d want=%d", mouse, i+1, offset, want)
			}
			if mouse {
				assertTabOverflowControls(t, out, 90)
			} else if out.TabLeftRect.W != 0 || out.TabRightRect.W != 0 || out.NewTabRect.W != 0 {
				t.Fatal("disabled mouse retains control targets")
			}
		}
	}
}
