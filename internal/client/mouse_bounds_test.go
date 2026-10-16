package client

import (
	"testing"

	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/tui"
)

func TestOutsideMouseLeavesClientDragPending(t *testing.T) {
	for _, pixel := range []bool{false, true} {
		m := &machineClient{size: tui.Winsize{Cols: 160, Rows: 45}}
		m.mouse = hostMouseState{pixel: pixel, cellW: 8, cellH: 16}
		drag := &tabFeedback{drop: 2, moved: true}
		m.tabFeedback = drag
		width, height := 160, 45
		if pixel {
			width, height = width*8, height*16
		}
		for _, kind := range []input.Kind{input.KindMouseClick, input.KindMouseMotion, input.KindMouseRelease} {
			for _, point := range [][2]int{{-1, 0}, {0, -1}, {width, 0}, {0, height}} {
				if err := m.input(input.Event{Kind: kind, Mouse: uv.Mouse{X: point[0], Y: point[1]}}); err != nil {
					t.Fatal(err)
				}
				if m.tabFeedback != drag || drag.drop != 2 {
					t.Fatal("outside event modified client feedback", pixel, kind, point)
				}
			}
		}
	}
}

func TestMergedPixelTabHitUsesOneCellConversion(t *testing.T) {
	state := model.New(t.TempDir(), 160, 45)
	snapshot := &render.Input{State: state, Panes: map[int]*render.PaneSnap{}}
	regions := render.Compose(*snapshot)
	if len(regions.TabHits) == 0 {
		t.Fatal("missing tab hit region")
	}
	hit := regions.TabHits[0]
	m := &machineClient{active: "local", size: tui.Winsize{Cols: 160, Rows: 45, Xpixel: 1280, Ypixel: 720}, regions: regions,
		endpoints: map[string]*managedEndpoint{"local": {snapshot: snapshot}}}
	m.mouse.Written([]byte("\x1b[?1016h"))
	raw := []byte("\x1b[<0;213;9M")
	event := input.Event{Kind: input.KindMouseClick, Raw: raw, Mouse: uv.Mouse{X: hit.X*8 + 3, Y: hit.Y*16 + 5, Button: uv.MouseLeft}}
	if err := m.input(event); err != nil {
		t.Fatal(err)
	}
	if m.tabFeedback == nil || m.tabFeedback.x != hit.X || m.tabFeedback.y != hit.Y {
		t.Fatalf("pixel tab hit was converted twice or rejected: %+v", m.tabFeedback)
	}
	if string(event.Raw) != string(raw) {
		t.Fatal("raw input changed")
	}
}
