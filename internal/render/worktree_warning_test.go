package render

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/model"
)

func TestEndpointWorktreeRejectionKeepsActionButtonsAndToast(t *testing.T) {
	for _, cols := range []int{100, 160} {
		s := model.New(t.TempDir(), cols, 25)
		s.RightSidebarMode = "hidden" // Keep the original pane geometry for this content regression.
		in := Input{State: s, ServerUI: true, ConfigWarningBanner: true, ConfigWarning: "ikesu worktree actions require a workspace inside a Git work tree."}
		base := Compose(Input{State: s, ServerUI: true})
		out := Compose(in)
		if c := out.Frame.Cell(34, 24); c.Content != "g" || c.Style.Fg != out.Frame.Palette.Red || c.Style.Bg != nil {
			t.Fatal("rejection did not occupy the observed action bar slot", c)
		}
		if cols == 100 {
			if out.Frame.Cell(56, 24).Content != "…" || *out.Frame.Cell(57, 24) != *base.Frame.Cell(57, 24) || *out.Frame.Cell(58, 24) != *base.Frame.Cell(58, 24) {
				t.Fatal("rejection did not leave the recorded two-cell gap before CYCLE")
			}
		}
		for _, r := range []struct{ x, w int }{{out.ActionBar.Copy.X, out.ActionBar.Copy.W}, {out.ActionBar.Cycle.X, out.ActionBar.Area.X + out.ActionBar.Area.W - out.ActionBar.Cycle.X}} {
			for x := r.x; x < r.x+r.w; x++ {
				if *out.Frame.Cell(x, 24) != *base.Frame.Cell(x, 24) {
					t.Fatal("rejection overwrote an action button", cols, x)
				}
			}
		}
		in.Settings = &Settings{Theme: "catppuccin"}
		out = Compose(in)
		if out.Frame.Cell(34, 24).Style.Attrs&uv.AttrFaint == 0 {
			t.Fatal("rejection did not dim behind settings")
		}
		in.Settings = nil
		in.Toast = &Toast{Title: "notice", Body: "context", Position: "top-right"}
		if out = Compose(in); out.ToastRect.Y != 0 {
			t.Fatal("bottom rejection pushed the toast outside the screen")
		}
		in.ServerUI = false
		in.Toast = nil
		out = Compose(in)
		if !strings.Contains(screenText(t, out, cols, 25), in.ConfigWarning) || out.ConfigWarningRect.Y != 0 {
			t.Fatal("local rejection banner changed")
		}
		in.SidebarConfig.Theme.Custom = map[string]string{"panel_bg": "transparent", "surface_dim": "#16161e"}
		out = Compose(in)
		if out.Frame.Cell(cols-1, 0).Style.Fg != out.Frame.Palette.SurfDim || out.Frame.Cell(cols-1, 0).Style.Fg == nil {
			t.Fatal("local rejection text lost its explicit contrast on a transparent panel")
		}
	}
}
