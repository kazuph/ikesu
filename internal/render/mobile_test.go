package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/model"
	"reflect"
	"strings"
	"testing"
)

func TestG2MobileSectionRowsNumberNameGitPriorityAndStyles(t *testing.T) {
	s := model.New("/tmp/name", 24, 30)
	s.MobileWidthThreshold = 64
	s.Apply(model.CmdSetSection{Index: 0, Section: model.SectionWork})
	s.Apply(model.CmdNewWorkspace{Cwd: "/tmp/other", Section: model.SectionPersonal})
	s.Apply(model.CmdSelectWorkspace{Index: 0})
	in := Input{State: s, MobileSwitcher: true, WorkspaceNames: map[int]string{0: "a-very-long-workspace-name"}, Git: map[int]GitStatus{0: {Branch: "a-long-branch-name", Ahead: 2, Behind: 1, Added: 42, Deleted: 3}}}
	rows := MobileRows(s, nil)
	if len(rows) != 12 || rows[0].Kind != RowMobileSpaces || rows[1].Kind != RowMobileNewWorkspace || rows[2].Kind != RowSection || rows[3].Kind != RowWorkspace || rows[4].Kind != RowGit {
		t.Fatal(rows)
	}
	out := Compose(in)
	line := func(y int) string {
		var b strings.Builder
		for x := 0; x < s.Cols; x++ {
			if c := out.Frame.Cell(x, y); c != nil {
				b.WriteString(c.Content)
			}
		}
		return b.String()
	}
	if strings.TrimRight(line(3), " ") != "  spaces" || strings.TrimRight(line(5), " ") != "  ▾ 💼 work" {
		t.Fatalf("header/blank: %q / %q", line(3), line(5))
	}
	if !strings.HasPrefix(line(6), "   · ") || !strings.HasSuffix(line(6), "…") {
		t.Fatal(line(6))
	}
	for _, cell := range []struct {
		x, y   int
		fg, bg any
		bold   bool
	}{{3, 6, Dim, Surface, false}, {5, 6, Text, Surface, true}, {2, 5, out.Frame.Palette.Overlay1, SurfDim, true}} {
		c := out.Frame.Cell(cell.x, cell.y)
		if !reflect.DeepEqual(c.Style.Fg, cell.fg) || (c.Style.Attrs&uv.AttrBold != 0) != cell.bold || !reflect.DeepEqual(c.Style.Bg, cell.bg) {
			t.Fatalf("cell %d/%d %+v", cell.x, cell.y, c)
		}
	}
	if !strings.HasPrefix(line(7), "   a-long-branch-name") || !strings.HasSuffix(line(7), "…") {
		t.Fatal(line(7))
	}
	if !reflect.DeepEqual(out.Frame.Cell(3, 7).Style.Fg, Dim) || !strings.Contains(line(10), "shell · tab 1") || !reflect.DeepEqual(out.Frame.Cell(3, 10).Style.Fg, Dim) {
		t.Fatal("branch and shell detail style")
	}
	s.Apply(model.CmdToggleSection{Section: model.SectionWork})
	for _, r := range MobileRows(s, nil) {
		if (r.Kind == RowWorkspace || r.Kind == RowGit) && r.Ws == 0 {
			t.Fatal("collapsed workspace survived")
		}
	}
	if !strings.HasPrefix(func() string { f := Compose(in).Frame; return f.Cell(2, 5).Content }(), "▸") {
		t.Fatal("collapsed arrow")
	}
	s.Cols = 65
	if s.Mobile() || s.PanesArea().X == 0 {
		t.Fatal("threshold boundary")
	}
	s.Cols = 64
	if !s.Mobile() || s.PanesArea().X != 0 || s.PanesArea().Y != 2 || s.SidebarArea().W != 0 {
		t.Fatal("mobile geometry")
	}
}
