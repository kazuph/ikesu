package render

import (
	"image/color"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
)

func TestTransparentPanelKeepsBrightControlTextAndConfirmationBackdrop(t *testing.T) {
	s := model.New(t.TempDir(), 160, 45)
	in := Input{State: s, ServerUI: true}
	in.SidebarConfig.Theme.Custom = map[string]string{"panel_bg": "transparent", "surface_dim": "#16161e"}
	base := Compose(in)
	in.Dialog = &Dialog{Title: "Stop server?", Detail: "Stops the gtx server and all running panes.", Confirm: "stop", Cancel: "cancel", Width: 76, CenterTerminal: true}
	confirmed := Compose(in)
	if confirmed.DialogRect != (layout.Rect{X: 42, Y: 20, W: 76, H: 5}) {
		t.Fatal("endpoint stop confirmation is not centered in the terminal", confirmed.DialogRect)
	}
	if confirmed.Frame.Cell(0, 0).Style.Attrs != base.Frame.Cell(0, 0).Style.Attrs|uv.AttrFaint {
		t.Fatal("endpoint stop confirmation must dim the sidebar header")
	}
	area := s.PanesArea()
	if confirmed.Frame.Cell(area.X, area.Y).Style.Attrs&uv.AttrFaint == 0 {
		t.Fatal("confirmation must dim the pane area")
	}
	contrast := color.RGBA{22, 22, 30, 255}
	button := confirmed.Frame.Cell(confirmed.DialogConfirm.X, confirmed.DialogConfirm.Y)
	if button.Style.Fg != contrast || button.Style.Bg != confirmed.Frame.Palette.Red {
		t.Fatal("confirmation text lost its explicit contrast color")
	}
	in.Dialog = nil
	in.ModeHint = " NAVIGATE "
	navigation := Compose(in).Frame
	if navigation.Cell(area.X, area.Y+area.H-1).Style.Fg != contrast {
		t.Fatal("navigation label text used the terminal default")
	}
	in.ModeHint = ""
	in.Menu = &Menu{Global: true, Items: []MenuItem{{Label: "new workspace"}}, Sel: 0, Hover: -1}
	menu := Compose(in)
	row := menu.MenuItemRows[0]
	if menu.Frame.Cell(row.X, row.Y).Style.Fg != contrast {
		t.Fatal("selected menu text used the terminal default")
	}
}
