package render

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/model"
)

func TestApprovalOverlayAndPaneAnswerIndicator(t *testing.T) {
	state := model.New(t.TempDir(), 120, 40)
	pane := state.FocusedPane()
	out := Compose(Input{State: state, Panes: map[int]*PaneSnap{}, ApprovalCounts: map[int]int{pane: 1}, Approval: &Approval{Pane: pane, Title: "guard approval", Lines: []string{"command: echo hello", "request: test", "1 approve   2 day allow   3 reject"}}})
	var text strings.Builder
	for _, cell := range out.Frame.Cells {
		text.WriteString(cell.Content)
	}
	for _, want := range []string{"Answer (1)", "guard approval", "command: echo hello", "request: test"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("render missing %q", want)
		}
	}
	if out.ApprovalRect.W == 0 || out.AnswerRects[pane].W == 0 {
		t.Fatal("missing approval or answer hit area")
	}
}

func TestReviewBorderlessIndicatorAvoidsWorkspaceCardHit(t *testing.T) {
	s := model.New(t.TempDir(), 120, 30)
	s.PaneBorders = false
	s.Apply(model.CmdNewWorkspace{Cwd: t.TempDir()})
	in := Input{State: s, Panes: map[int]*PaneSnap{}}
	f := Compose(in).Frame
	y := -1
	for _, p := range SidebarViewForInput(in).Rows {
		if !p.Right && p.Row.Kind == RowCardBlank && p.Row.Ws != s.Active {
			y = p.Y
			break
		}
	}
	if y < 0 {
		t.Fatal("inactive workspace blank card not found")
	}
	for _, a := range []model.Rect{s.ActionBarArea(), s.TabBarArea(), s.SidebarArea()} {
		for row := a.Y; row < a.Y+a.H; row++ {
			for x := a.X; x < a.X+a.W; x++ {
				if row == y && x < s.SidebarArea().X+s.SidebarArea().W-1 {
					f.Set(x, row, " ", uv.Style{})
				} else {
					f.Set(x, row, "x", uv.Style{})
				}
			}
		}
	}
	if r, ok := approvalBorderlessSlot(f, in, &Output{}, 10); ok {
		t.Fatalf("indicator overlaps workspace card click target: %+v", r)
	}
}

func TestReviewBorderlessAnswerDoesNotCoverPaneContent(t *testing.T) {
	state := model.New(t.TempDir(), 120, 40)
	state.PaneBorders = false
	pane := state.FocusedPane()
	out := Compose(Input{State: state, Panes: map[int]*PaneSnap{}, ApprovalCounts: map[int]int{pane: 1}})
	rect := out.AnswerRects[pane]
	for _, p := range state.DisplayPaneRects() {
		if p.ID != pane {
			continue
		}
		inner, _ := model.PaneContentRect(p.Rect, false)
		if rect.W == 0 || (rect.X < inner.X+inner.W && rect.X+rect.W > inner.X && rect.Y < inner.Y+inner.H && rect.Y+rect.H > inner.Y) {
			t.Fatalf("answer=%+v overlaps content=%+v", rect, inner)
		}
	}
}

func TestReviewBorderlessIndicatorFitsNarrowWindow(t *testing.T) {
	for _, cols := range []int{45, 80, 120} {
		state := model.New(t.TempDir(), cols, 20)
		state.PaneBorders = false
		pane := state.FocusedPane()
		plain := Compose(Input{State: state, Panes: map[int]*PaneSnap{}})
		out := Compose(Input{State: state, Panes: map[int]*PaneSnap{}, ApprovalCounts: map[int]int{pane: 1}})
		r := out.AnswerRects[pane]
		if r.W < len([]rune("▲ Answer (1)")) {
			t.Errorf("cols=%d indicator too narrow: %+v", cols, r)
		}
		for _, b := range []model.Rect{plain.ActionBar.Copy, plain.ActionBar.Cycle, plain.ActionBar.Rotate, plain.ActionBar.Equalize} {
			if b.W > 0 && b.Y == r.Y && r.X < b.X+b.W && r.X+r.W > b.X {
				t.Errorf("cols=%d indicator %+v overlaps button %+v", cols, r, b)
			}
		}
	}
}

func TestApprovalColorsSelectionAndHeight(t *testing.T) {
	for _, late := range []bool{false, true} {
		s := model.New(t.TempDir(), 120, 40)
		pane := s.FocusedPane()
		f := NewFrame(120, 40)
		f.Palette = ThemePalette("tokyo-night")
		in := Input{State: s, Approval: &Approval{Pane: pane, Title: " title", Late: late, Lines: []string{" details", " 1  option", " confirm ↵"}, Kinds: []ApprovalLineKind{{Role: "subtitle"}, {Selected: true}, {Role: "footer"}}}}
		var out Output
		drawApproval(f, in, &out)
		r := out.ApprovalRect
		want := f.Palette.Red
		if late {
			want = f.Palette.Accent
		}
		if got := f.Cell(r.X, r.Y).Style.Fg; got != want {
			t.Errorf("late=%t border=%v want %v", late, got, want)
		}
		if r.H != len(in.Approval.Lines)+4 {
			t.Errorf("late=%t height=%d", late, r.H)
		}
		selected := f.Cell(r.X+2, r.Y+3)
		if selected.Style.Bg != f.Palette.Accent || selected.Style.Fg != f.Palette.SurfDim {
			t.Errorf("late=%t selected style=%+v", late, selected.Style)
		}
	}
}

func TestApprovalIndicatorColorPrefersGuard(t *testing.T) {
	s := model.New(t.TempDir(), 120, 40)
	pane := s.FocusedPane()
	for _, guard := range []bool{false, true} {
		f := NewFrame(120, 40)
		f.Palette = ThemePalette("tokyo-night")
		in := Input{State: s, ApprovalCounts: map[int]int{pane: 2}, ApprovalGuardPanes: map[int]bool{pane: guard}}
		var out Output
		drawApprovalIndicators(f, in, &out)
		r := out.AnswerRects[pane]
		want := f.Palette.Accent
		if guard {
			want = f.Palette.Red
		}
		if got := f.Cell(r.X, r.Y).Style.Bg; got != want {
			t.Errorf("guard=%t background=%v want %v", guard, got, want)
		}
	}
}

func TestApprovalSurfaceAndFooterUseThemeColors(t *testing.T) {
	s := model.New(t.TempDir(), 120, 40)
	f := NewFrame(120, 40)
	f.Palette = ThemePalette("tokyo-night")
	in := Input{State: s, Approval: &Approval{Pane: s.FocusedPane(), Title: " title", Lines: []string{" $ echo", " text t  ·  confirm ↵"}, Kinds: []ApprovalLineKind{{Role: "surface"}, {Role: "footer"}}}}
	var out Output
	drawApproval(f, in, &out)
	r := out.ApprovalRect
	command := f.Cell(r.X+2, r.Y+2)
	if command.Style.Bg != f.Palette.Surface || command.Style.Attrs&uv.AttrBold == 0 {
		t.Errorf("command style=%+v", command.Style)
	}
	label := f.Cell(r.X+2, r.Y+3)
	key := f.Cell(r.X+7, r.Y+3)
	if label.Style.Fg != f.Palette.Dim || key.Content != "t" || key.Style.Fg != f.Palette.Text {
		t.Errorf("label=%+v key=%+v", label, key)
	}
}
