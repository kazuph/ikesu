package render

import (
	"reflect"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
)

func TestSPECActionBarGeometryStyleAndLabels(t *testing.T) {
	for _, label := range []string{" COPY ", " PHONE COPY ", " EXIT COPY "} {
		s := testState(160, 45)
		out := Compose(Input{State: s, CopyLabel: label})
		a := out.ActionBar
		if a.Area.H != 1 || a.Area.Y != s.PanesArea().Y+s.PanesArea().H || a.Copy.W != len([]rune(label)) {
			t.Fatal("bar geometry", a)
		}
		if c := out.Frame.Cell(a.Area.X, a.Area.Y); c.Style.Bg != nil {
			t.Fatal("copy background", c.Style)
		}
		for _, r := range []layout.Rect{a.Cycle, a.Rotate, a.Equalize} {
			for x := r.X; x < r.X+r.W; x++ {
				c := out.Frame.Cell(x, r.Y)
				if !reflect.DeepEqual(c.Style.Fg, Accent) || c.Style.Bg != nil || c.Style.Attrs != uv.AttrBold || c.Style.Underline != uv.UnderlineSingle {
					t.Fatal("button style", c.Style)
				}
			}
		}
		if a.Rotate.X != a.Cycle.X+a.Cycle.W+1 || a.Equalize.X != a.Rotate.X+a.Rotate.W+1 || a.Equalize.X+a.Equalize.W != a.Area.X+a.Area.W-1 {
			t.Fatal("gaps/right margin", a)
		}
	}
	for width := 1; width <= 45; width++ {
		s := testState(model.SidebarWidth+width, 30)
		out := Compose(Input{State: s})
		for _, r := range []layout.Rect{out.ActionBar.Cycle, out.ActionBar.Rotate, out.ActionBar.Equalize} {
			if r.W != 0 && (r.X < out.ActionBar.Area.X || r.X+r.W > out.ActionBar.Area.X+width) {
				t.Fatal("partial hit area", width, r)
			}
		}
	}
}

func TestSPECFrameThresholdGutterAndTitle(t *testing.T) {
	for _, borders := range []bool{false, true} {
		for w := 1; w <= 10; w++ {
			for h := 1; h <= 5; h++ {
				r := layout.Rect{X: 3, Y: 4, W: w, H: h}
				inner, gutter := model.PaneContentRect(r, borders)
				want := r
				g := 0
				if borders && w > 2 && h > 2 {
					want = layout.Rect{X: 4, Y: 5, W: w - 2, H: h - 2}
					if w-2 >= 5 {
						g = 1
					}
				}
				if inner != want || gutter != g {
					t.Fatalf("%v %dx%d inner=%v gutter=%d", borders, w, h, inner, gutter)
				}
			}
		}
	}
	for w := 1; w <= 20; w++ {
		pr := layout.PaneRect{ID: 5, Rect: layout.Rect{W: w}}
		snap := &PaneSnap{Label: "日本語abcdef"}
		got := paneTitle(pr, snap, &model.Tab{})
		full := "%5 日本語abcdef"
		want := full
		if w <= 4 {
			want = ""
		} else if len([]rune(full)) > w-4 {
			want = string([]rune(full)[:w-5]) + "…"
		}
		if got != want {
			t.Fatalf("width=%d title=%q want=%q", w, got, want)
		}
	}
	s := testState(90, 30)
	pid := s.ActiveWorkspace().TabRef().Layout.Focused()
	out := Compose(Input{State: s, Panes: map[int]*PaneSnap{pid: blankSnap()}})
	row := screenText(t, out, 90, 30)
	if !strings.Contains(row, " %1 shell ") {
		t.Fatal("single pane title", row)
	}
}

func TestSPECRestoreLabelThresholdAndPriority(t *testing.T) {
	for _, agent := range []string{"", "codex"} {
		for _, width := range []int{33, 34, 50} {
			for _, msg := range []string{"saved session", "no saved session"} {
				s := testState(model.SidebarWidth+width, 20)
				pid := s.ActiveWorkspace().TabRef().Layout.Focused()
				snap := &PaneSnap{Label: strings.Repeat("a", 50), Agent: agent, Restore: msg}
				out := Compose(Input{State: s, Panes: map[int]*PaneSnap{pid: snap}})
				text := screenText(t, out, s.Cols, s.Rows)
				if strings.Contains(text, " "+msg+" ") != (agent != "" && width >= 34) {
					t.Fatalf("agent=%q width=%d label=%q screen=%s", agent, width, msg, text)
				}
			}
		}
	}
}

func TestSPECCopyCursorStyleAndOutOfRange(t *testing.T) {
	s := testState(90, 30)
	pid := s.ActiveWorkspace().TabRef().Layout.Focused()
	r := s.ActiveWorkspace().TabRef().PaneRects(s.PanesArea())[0].Rect
	inner, g := model.PaneContentRect(r, true)
	for _, pos := range [][2]int{{0, 0}, {inner.W - g - 1, inner.H - 1}, {-1, 0}, {inner.W - g, 0}, {0, -1}, {0, inner.H}} {
		base := Compose(Input{State: s, Panes: map[int]*PaneSnap{pid: blankSnap()}})
		out := Compose(Input{State: s, Panes: map[int]*PaneSnap{pid: blankSnap()}, Copy: &CopyMark{Pane: pid, X: pos[0], Y: pos[1]}})
		count := 0
		for i, c := range out.Frame.Cells {
			if !reflect.DeepEqual(c.Style, base.Frame.Cells[i].Style) {
				count++
				if !reflect.DeepEqual(c.Style.Fg, Surface) || !reflect.DeepEqual(c.Style.Bg, Accent) || c.Style.Attrs != uv.AttrBold {
					t.Fatal("cursor style", c.Style)
				}
			}
		}
		want := 0
		if pos[0] >= 0 && pos[0] < inner.W-g && pos[1] >= 0 && pos[1] < inner.H {
			want = 1
		}
		if count != want {
			t.Fatalf("cursor %v changed %d cells want %d", pos, count, want)
		}
	}
}

func TestSPECFullscreenCopyHasNoFrameOrGutter(t *testing.T) {
	s := testState(90, 30)
	l := s.ActiveWorkspace().TabRef().Layout
	pid := l.Focused()
	l.SplitFocused(layout.Vertical, pid+1)
	area := s.PanesArea()
	grid := make([][]uv.Cell, area.H)
	for y := range grid {
		grid[y] = make([]uv.Cell, area.W)
		for x := range grid[y] {
			grid[y][x] = uv.Cell{Content: "T", Width: 1}
		}
	}
	for _, pos := range [][2]int{{-1, 0}, {area.W, 0}, {0, -1}, {0, area.H}, {1, 1}} {
		out := Compose(Input{State: s, Panes: map[int]*PaneSnap{pid: {Grid: grid, Label: "hidden", Agent: "codex", Restore: "saved session", ScrollN: 500}}, Copy: &CopyMark{Pane: pid, Fullscreen: true, X: pos[0], Y: pos[1]}})
		for y := area.Y; y < area.Y+area.H; y++ {
			for x := area.X; x < area.X+area.W; x++ {
				c := out.Frame.Cell(x, y)
				if c.Content != "T" {
					t.Fatalf("fullscreen frame/gutter at %d,%d: %+v", x, y, c)
				}
				accent := pos[0] >= 0 && pos[0] < area.W && pos[1] >= 0 && pos[1] < area.H && x == area.X+pos[0] && y == area.Y+pos[1]
				if reflect.DeepEqual(c.Style.Bg, Accent) != accent {
					t.Fatalf("fullscreen cursor %v at %d,%d", pos, x, y)
				}
			}
		}
	}
}

func TestSPECTabVisibilityAndEmptyHitAreas(t *testing.T) {
	for _, show := range []bool{false, true} {
		for _, hideSingle := range []bool{false, true} {
			for _, tabs := range []int{1, 2} {
				s := testState(90, 30)
				s.ShowTabBar = show
				s.HideTabBarWhenSingleTab = hideSingle
				if tabs == 2 {
					s.Apply(model.CmdNewTab{Workspace: 0})
				}
				out := Compose(Input{State: s})
				visible := show && (!hideSingle || tabs > 1)
				if (len(out.TabHits) > 0) != visible || (out.NewTabRect.W > 0) != visible {
					t.Fatal("tab hits", show, hideSingle, tabs, out.TabHits)
				}
				if out.ActionBar.Area.Y != 29 || out.ActionBar.Area.H != 1 {
					t.Fatal("bottom bar", out.ActionBar)
				}
			}
		}
	}
}
