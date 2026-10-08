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
	rows := MobileRows(s)
	if len(rows) != 8 || rows[0].Kind != RowSection || rows[1].Kind != RowBlank || rows[2].Kind != RowWorkspace || rows[3].Kind != RowGit {
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
	if strings.TrimRight(line(3), " ") != "▾ 💼 work" || strings.TrimSpace(line(4)) != "" {
		t.Fatalf("header/blank: %q / %q", line(3), line(4))
	}
	if !strings.HasPrefix(line(5), "  · 1 ") || !strings.HasSuffix(line(5), "…") {
		t.Fatal(line(5))
	}
	for _, p := range []struct {
		x, y int
		fg   any
	}{{4, 5, Dim}, {6, 5, Text}, {0, 3, Dim}} {
		c := out.Frame.Cell(p.x, p.y)
		if !reflect.DeepEqual(c.Style.Fg, p.fg) || c.Style.Attrs&uv.AttrBold == 0 || !reflect.DeepEqual(c.Style.Bg, SurfDim) {
			t.Fatalf("cell %d/%d %+v", p.x, p.y, c)
		}
	}
	if !strings.HasPrefix(line(6), "    ↑2 ↓1 +42 -3 ") || !strings.HasSuffix(line(6), "…") {
		t.Fatal(line(6))
	}
	if !reflect.DeepEqual(out.Frame.Cell(4, 6).Style.Fg, Mauve) || !strings.Contains(line(10), "shell") || !reflect.DeepEqual(out.Frame.Cell(4, 10).Style.Fg, Dim) {
		t.Fatal("active/inactive git style or shell fallback")
	}
	s.Apply(model.CmdToggleSection{Section: model.SectionWork})
	for _, r := range MobileRows(s) {
		if (r.Kind == RowWorkspace || r.Kind == RowGit) && r.Ws == 0 {
			t.Fatal("collapsed workspace survived")
		}
	}
	if !strings.HasPrefix(func() string { f := Compose(in).Frame; return f.Cell(0, 3).Content }(), "▸") {
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
