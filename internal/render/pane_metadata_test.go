package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/model"
	"reflect"
	"strings"
	"testing"
)

func TestG2PaneCustomMetadataStyledTokenAndStateLabels(t *testing.T) {
	s := model.New("/tmp/repo", 120, 35)
	fg := "#abc"
	bold := true
	var c config.Config
	c.UI.Sidebar.Agents.Rows = [][]config.SidebarToken{{{Token: "pane"}, {Token: "$custom", Fg: &fg, Bold: &bold}, {Token: "agent"}, {Token: "state_text"}}}
	in := Input{State: s, SidebarConfig: c, Panes: map[int]*PaneSnap{1: {Label: "PN", Agent: "codex", DisplayAgent: "Coder", Status: StatusGlyph{Key: "working", Rune: "◐", Color: Yellow}, StateLabels: map[string]string{"working": "Building"}, Tokens: map[string]string{"custom": "VALUE"}}}}
	frame := Compose(in).Frame
	y := 2
	right := s.RightSidebarArea()
	var text strings.Builder
	for x := right.X + 1; x < right.X+right.W; x++ {
		if cell := frame.Cell(x, y); cell != nil {
			text.WriteString(cell.Content)
		}
	}
	if !strings.Contains(text.String(), "PN") || strings.Contains(text.String(), "%1") || !strings.Contains(text.String(), "VALUE") || !strings.Contains(text.String(), "Coder") {
		t.Fatal(text.String())
	}
	x := strings.Index(text.String(), "VALUE")
	cell := frame.Cell(right.X+1+x, y)
	if !reflect.DeepEqual(cell.Style.Fg, tokenStyle(config.SidebarToken{Fg: &fg}, uv.Style{}).Fg) || cell.Style.Attrs&uv.AttrBold == 0 {
		t.Fatal("explicit custom token style lost", cell.Style)
	}
	in.Panes[1].Tokens = map[string]string{}
	frame = Compose(in).Frame
	for x := right.X + 1; x < right.X+right.W; x++ {
		if frame.Cell(x, y).Content == "V" {
			t.Fatal("cleared custom token survived")
		}
	}
}
