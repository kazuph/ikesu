package render

import (
	"image"
	"testing"

	"github.com/kazuph/goterrace/internal/kitty"
	"github.com/kazuph/goterrace/internal/layout"
)

func TestOverlaysHideOnlyCoveredImageCells(t *testing.T) {
	s := testState(160, 45)
	pid := s.ActiveWorkspace().TabRef().Layout.Focused()
	snap := blankSnap()
	for i, p := range []kitty.Placement{{X: 6, Y: 4, Cols: 40, Rows: 20}, {X: 100, Y: 30, Cols: 8, Rows: 4}} {
		p.Image = kitty.Image{ID: uint32(i + 1), Format: 24, Width: 8, Height: 4, Data: make([]byte, 96)}
		snap.Graphics = append(snap.Graphics, p)
	}
	base := Input{State: s, Panes: map[int]*PaneSnap{pid: snap}}
	original := Compose(base).Frame.Graphics
	rect := func(r layout.Rect) image.Rectangle { return image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H) }
	for _, name := range []string{"menu-away", "menu-overlap", "toast", "dialog", "help", "settings", "navigator"} {
		t.Run(name, func(t *testing.T) {
			in := base
			switch name {
			case "menu-away":
				in.Menu = &Menu{X: 145, Y: 10, Items: []MenuItem{{Label: "Close pane"}}}
			case "menu-overlap":
				in.Menu = &Menu{X: 50, Y: 15, Items: []MenuItem{{Label: "Close pane"}}}
			case "toast":
				in.Toast = &Toast{Title: "notice"}
			case "dialog":
				in.Dialog = &Dialog{Title: "Confirm"}
			case "help":
				in.Help = &HelpInput{}
			case "settings":
				in.Settings = &Settings{}
			case "navigator":
				in.Navigator = &Navigator{}
			}
			out := Compose(in)
			covered := rect(out.MenuRect).Union(rect(out.ToastRect)).Union(rect(out.DialogRect)).Union(rect(out.HelpRect))
			if in.Settings != nil {
				covered = rect(SettingsRect(160, 45))
			}
			if in.Navigator != nil {
				covered = rect(NavigatorRect(160, 45))
			}
			expected := map[image.Point]uint32{}
			for _, g := range original {
				p := g.Placement
				for y := p.Y; y < p.Y+p.Rows; y++ {
					for x := p.X; x < p.X+p.Cols; x++ {
						point := image.Pt(x, y)
						if !point.In(covered) {
							expected[point] = p.Image.ID
						}
					}
				}
			}
			actual := map[image.Point]uint32{}
			for _, g := range out.Frame.Graphics {
				p := g.Placement
				for y := p.Y; y < p.Y+p.Rows; y++ {
					for x := p.X; x < p.X+p.Cols; x++ {
						point := image.Pt(x, y)
						if point.In(covered) {
							t.Fatalf("image leaked through opaque UI at %v", point)
						}
						if _, duplicate := actual[point]; duplicate {
							t.Fatalf("overlapping image fragments at %v", point)
						}
						actual[point] = p.Image.ID
					}
				}
			}
			if len(actual) != len(expected) {
				t.Fatalf("visible image cells=%d want %d", len(actual), len(expected))
			}
			for point, id := range expected {
				if actual[point] != id {
					t.Fatalf("uncovered image missing at %v", point)
				}
			}
		})
	}
}

func TestOverlayCropKeepsRemainingSourcePixels(t *testing.T) {
	f := NewFrame(160, 45)
	p := kitty.Placement{Image: kitty.Image{ID: 51, Width: 8, Height: 4}, X: 33, Y: 6, Cols: 40, Rows: 20}
	f.Graphics = []Graphic{{Pane: 1, Placement: p}}
	f.coverGraphics(image.Rect(49, 14, 80, 45))
	if len(f.Graphics) != 2 {
		t.Fatalf("fragments=%d want 2", len(f.Graphics))
	}
	expected := []image.Rectangle{image.Rect(0, 0, 8, 1), image.Rect(0, 1, 3, 4)}
	for i, g := range f.Graphics {
		if g.Source != expected[i] {
			t.Fatalf("fragment %d source=%v want %v", i, g.Source, expected[i])
		}
	}
}
