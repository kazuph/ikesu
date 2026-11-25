package render

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/ikesu/internal/layout"
)

func TestRightSidebarPluginReservesRowsForAgentsJobsAndKoi(t *testing.T) {
	for _, jobs := range []bool{false, true} {
		in := koiFixture(t, 4)
		in.State.Rows = 26
		in.JobsPanel = jobs
		for range 40 {
			in.Jobs = append(in.Jobs, JobRow{Label: "job-row"})
		}
		panel := &SidebarPanel{Sections: []SidebarPanelSection{{Key: "right/status.30s.sh", Title: "Right plugin"}}}
		for range 30 {
			panel.Sections[0].Lines = append(panel.Sections[0].Lines, SidebarPanelLine{Text: "\x1b[31mPlugin output\x1b[0m"})
		}
		in.RightSidebarPanel = panel
		reserved := RightSidebarPanelRect(in.State, panel)
		view := SidebarViewForInput(in)
		if reserved.H != 12 || reserved.Y != 14 || view.Agents.Rect.Y+view.Agents.Rect.H != reserved.Y {
			t.Fatalf("right panel = %+v, list = %+v", reserved, view.Agents.Rect)
		}
		for _, row := range view.Rows {
			if row.Right && row.Y >= reserved.Y {
				t.Fatalf("list row overlaps plugin: %+v", row)
			}
		}
		machines := &MachineSidebar{Groups: []MachineGroup{{ID: "one", Input: &in}, {ID: "two", Input: &in}}, RightScroll: 100}
		for _, sidebar := range []*MachineSidebar{nil, machines} {
			f := ComposeMachines(in, sidebar).Frame
			var heading strings.Builder
			for x := reserved.X; x < reserved.X+reserved.W; x++ {
				heading.WriteString(f.Cell(x, reserved.Y).Content)
			}
			if !strings.Contains(heading.String(), "Right plugin") {
				t.Fatalf("right plugin heading = %q", heading.String())
			}
			if got := f.Cell(reserved.X+1, reserved.Y+1); got.Content != "P" || got.Style.Fg != ansi.BasicColor(1) {
				t.Fatalf("right plugin did not render SGR text: %+v", got)
			}
			scene := f.KoiScene
			if scene.Pond.Y+scene.Pond.H != reserved.Y || scene.ClickPond.Y+scene.ClickPond.H > reserved.Y {
				t.Fatalf("koi overlaps plugin: pond=%+v click=%+v", scene.Pond, scene.ClickPond)
			}
			if !jobs && len(scene.Fish) == 0 {
				t.Fatal("fixture must have fish in the agents panel")
			}
			if sidebar != nil {
				for _, hit := range sidebar.Hits {
					if hit.Right && hit.Rect.Y+hit.Rect.H > reserved.Y {
						t.Fatalf("machine hit overlaps plugin: %+v", hit)
					}
				}
			}
		}
	}
}

func TestJobsFramesKeepOnlyMiniKoi(t *testing.T) {
	for _, retained := range []bool{false, true} {
		in := koiFixture(t, 1)
		in.Panes[1].Status.Key = "working"
		in.KoiKitty, in.KoiCellW, in.KoiCellH = true, 8, 16
		in.SidebarConfig.UI.TransparentSidebarCards = true
		in.SidebarConfig.Theme.Custom = map[string]string{"panel_bg": "transparent"}
		in.Jobs = []JobRow{{Label: "clock", Elapsed: "1s"}}
		now := time.Now()
		var clock KoiAnimation
		clock.Apply(&in, nil, now, time.Minute)
		scene := Compose(in).Frame.KoiScene
		if !clock.Click(scene, true, scene.ClickPond.X, scene.ClickPond.Y, now) {
			t.Fatal("could not set up ripple in agents view")
		}
		clock.Apply(&in, nil, now, time.Minute)
		// Advance resident motion enough to make both the right fish and left
		// mini fish visible, while the ripple still has its first frames.
		clock.Apply(&in, nil, now.Add(KoiInterval), time.Minute)
		agents := Compose(in).Frame
		seen := map[int]bool{}
		for _, g := range agents.Graphics {
			seen[g.Pane] = true
		}
		if !seen[koiPondPane] || !seen[koiMiniPane] || !seen[koiRipplePane] {
			t.Fatalf("fixture missing a koi placement: %v", seen)
		}
		var enc Encoder
		enc.Frame(agents, 0, 0, false)
		before := in
		in.JobsPanel = true
		clock.ApplyFrame(&in, before, nil, now.Add(time.Second), time.Minute)
		f := Compose(in).Frame
		if retained {
			f = f.Viewport(f.W-1, f.H)
			f.RetainGraphics = true
		}
		data := enc.Frame(f, 0, 0, false)
		if bytes.Count(data, []byte("a=d,")) != 2 || bytes.Contains(data, []byte("a=p,")) {
			t.Fatal("switch to Jobs must delete only pond fish and ripple")
		}
		for _, placement := range enc.koi.placements {
			if placement.Pane != koiMiniPane {
				t.Fatalf("right placement survived Jobs, pane=%d", placement.Pane)
			}
		}
		if retained {
			continue
		}
		before = in
		in.Jobs = []JobRow{{Label: "clock", Elapsed: "2s"}}
		clock.ApplyFrame(&in, before, nil, now.Add(2*time.Second), time.Minute)
		next := Compose(in).Frame
		if reflect.DeepEqual(f.Cells, next.Cells) {
			t.Fatal("job elapsed time did not repaint")
		}
		data = enc.Frame(next, 0, 0, false)
		if bytes.Contains(data, []byte("\x1b_G")) {
			t.Fatal("job elapsed paint changed koi placements")
		}
		clock.Apply(&in, nil, now.Add(2*time.Second+KoiInterval), time.Minute)
		animated := AnimateKoi(next.KoiScene, in)
		if len(animated.KoiScene.Fish) != 0 || animated.KoiScene.Ripple != nil || len(animated.KoiScene.Mini) == 0 {
			t.Fatal("animation did not preserve the Jobs-only koi policy")
		}
	}
}

func TestRightSidebarPluginGeometryAtNormalAndCompactWidths(t *testing.T) {
	for _, compact := range []bool{false, true} {
		in := koiFixture(t, 5)
		in.State.SidebarMin, in.State.RightSidebarSize = 16, 16
		if compact {
			in.State.RightSidebarMode, in.State.RightSidebarSize = "compact", 0
		}
		p := &SidebarPanel{Sections: []SidebarPanelSection{{Key: "probe", Title: "Probe", Lines: []SidebarPanelLine{{Text: "body"}}}}}
		in.RightSidebarPanel = p
		area := in.State.RightSidebarArea()
		for _, machines := range []*MachineSidebar{nil, {Groups: []MachineGroup{{ID: "a", Input: &in}, {ID: "b", Input: &in}}}} {
			frame := ComposeMachines(in, machines).Frame
			r := RightSidebarPanelRect(in.State, p)
			list := SidebarViewForInput(in).Agents.Rect
			if compact {
				if area.W != 2 || r.H != 0 || list.Y+list.H != in.State.Rows {
					t.Fatalf("compact geometry: area=%+v panel=%+v list=%+v", area, r, list)
				}
				continue
			}
			if area.W != 16 || r.W != 15 || r.H != 2 || list.Y+list.H != r.Y {
				t.Fatalf("normal width geometry: area=%+v panel=%+v list=%+v", area, r, list)
			}
			var heading strings.Builder
			for x := r.X; x < r.X+r.W; x++ {
				heading.WriteString(frame.Cell(x, r.Y).Content)
			}
			if !strings.Contains(heading.String(), "Probe") || frame.Cell(r.X+1, r.Y+1).Content != "b" {
				t.Fatalf("missing narrow plugin drawing: %q, %+v", heading.String(), frame.Cell(r.X+1, r.Y+1))
			}
			scene := frame.KoiScene
			if len(scene.Fish) == 0 || scene.Pond.Y+scene.Pond.H != r.Y || scene.ClickPond.Y+scene.ClickPond.H > r.Y {
				t.Fatalf("narrow pond overlaps panel: pond=%+v click=%+v fish=%d", scene.Pond, scene.ClickPond, len(scene.Fish))
			}
		}
	}
}

func TestSidebarPluginANSIRejectsMalformedDecorationOnBothSides(t *testing.T) {
	in := koiFixture(t, 0)
	in.State.SidebarPanelRows = 2
	p := &SidebarPanel{Sections: []SidebarPanelSection{{Key: "probe", Title: "Probe", Lines: []SidebarPanelLine{
		{Text: "\x1bmA\x1b[?31mB\x1b[123\x1b[32mC\x1b[0mD\x1b["},
	}}}}
	in.SidebarPanel, in.RightSidebarPanel = p, p
	frame := Compose(in).Frame
	for _, r := range []layout.Rect{leftSidebarPanelRect(in.State), RightSidebarPanelRect(in.State, p)} {
		for i, want := range []string{"A", "B", "C", "D"} {
			cell := frame.Cell(r.X+1+i, r.Y+1)
			fg := frame.Palette.Text
			if want == "C" {
				fg = ansi.BasicColor(2)
			}
			if cell.Content != want || cell.Style.Fg != fg {
				t.Fatalf("side at X=%d cell %s = %+v, color want %v", r.X, want, cell, fg)
			}
		}
		if got := frame.Cell(r.X+5, r.Y+1).Content; got != " " {
			t.Fatalf("incomplete sequence rendered as text: %q", got)
		}
	}
}
