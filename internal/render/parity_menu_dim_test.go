package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"testing"
)

func TestLocalMenuDimsInactivePaneAndClosingRestoresIt(t *testing.T) {
	for _, server := range []bool{false, true} {
		s := model.New(t.TempDir(), 160, 45)
		s.Apply(model.CmdSplitFocused{Dir: layout.Vertical})
		in := Input{State: s, ServerUI: server, Menu: &Menu{X: 27, Y: 0, Items: []MenuItem{{Label: "New tab"}}}}
		f := Compose(in).Frame
		want := uint8(uv.AttrFaint)
		if server {
			want = 0
		}
		if f.Cell(30, 20).Style.Attrs != want || f.Cell(120, 20).Style.Attrs != 0 {
			t.Fatalf("server=%t: inactive/focused pane menu dim differs", server)
		}
		in.Menu = nil
		if Compose(in).Frame.Cell(30, 20).Style.Attrs != 0 {
			t.Fatal("menu dismissal retained inactive pane dim")
		}
	}
}
