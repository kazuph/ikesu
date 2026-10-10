package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/model"
	"reflect"
	"strings"
	"testing"
)

func TestMobileEmptySummaryUsesObservedNarrowHeaderPadding(t *testing.T) {
	for _, server := range []bool{false, true} {
		for _, tc := range []struct {
			width int
			label string
		}{{20, "  …       "}, {21, " no agents "}} {
			s := model.New(t.TempDir(), tc.width, 25)
			s.MobileWidthThreshold = 64
			f := Compose(Input{State: s, ServerUI: server}).Frame
			if tc.width == 20 && f.Cell(1, 1).Style.Fg != f.Palette.Dim {
				t.Fatal("collapsed agent summary changed the preceding blank's color")
			}
			if tc.width == 20 && f.Cell(2, 1).Style.Fg != f.Palette.Dim {
				t.Fatal("collapsed agent summary changed the ellipsis color")
			}
			for x, ch := range []rune(tc.label) {
				if f.Cell(x, 1).Content != string(ch) {
					t.Fatalf("width %d server=%t: summary at %d", tc.width, server, x)
				}
			}
		}
	}
}

func TestMobileNarrowHeaderReservesGapBeforeTabCount(t *testing.T) {
	for _, tc := range []struct {
		width int
		name  string
	}{{24, "pa…"}, {25, "par…"}, {26, "pari…"}, {27, "parity"}} {
		s := model.New(t.TempDir(), tc.width, 25)
		s.MobileWidthThreshold = 64
		f := Compose(Input{State: s, ServerUI: true, WorkspaceNames: map[int]string{0: "parity"}}).Frame
		end := 3 + len([]rune(tc.name))
		for i, ch := range []rune(tc.name) {
			if f.Cell(3+i, 0).Content != string(ch) {
				t.Fatalf("width %d: workspace name differs at %d", tc.width, i)
			}
		}
		if c := f.Cell(end, 0); c.Content != " " || c.Style.Attrs&uv.AttrBold != 0 {
			t.Fatalf("width %d: name touches tab count", tc.width)
		}
	}
}

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
