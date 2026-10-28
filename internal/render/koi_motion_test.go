package render

import (
	"math"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/layout"
)

func TestKoiMotionIdleAndDoneKeepSwimming(t *testing.T) {
	for _, state := range []string{"idle", "done"} {
		t.Run(state, func(t *testing.T) {
			in := Input{KoiCellW: 8, KoiCellH: 16, KoiMotion: map[string]koiMotion{}}
			pond := layout.Rect{X: 130, Y: 2, W: 29, H: 60}
			agent := KoiAgent{ID: "local:1", Seed: 1, State: state}
			first := koiPondPose(agent, pond, in)
			last := first
			for tick := 1; tick <= 400; tick++ {
				in.KoiTime = float64(tick) * KoiInterval.Seconds()
				last = koiPondPose(agent, pond, in)
				if !last.Swimming || in.KoiMotion[agent.ID].Speed <= 0 {
					t.Fatal("fish stopped", tick)
				}
			}
			if math.Hypot(last.X-first.X, last.Y-first.Y) == 0 {
				t.Fatal("fish never moved")
			}
			if (KoiScene{Fish: []KoiFish{last}}).UpdateInterval() != KoiInterval {
				t.Fatal("lost 25ms clock")
			}
		})
	}
}

func TestKoiMotionResizeRetainsPositionAndSpeedLimit(t *testing.T) {
	in := Input{KoiCellW: 8, KoiCellH: 16, KoiMotion: map[string]koiMotion{}}
	agent := KoiAgent{ID: "local:1", Seed: 1, State: "working"}
	pond := layout.Rect{X: 130, Y: 2, W: 29, H: 60}
	old := koiPondPose(agent, pond, in)
	in.KoiTime = KoiInterval.Seconds()
	pond.H--
	next := koiPondPose(agent, pond, in)
	dots := math.Hypot((next.X-old.X)*8, (next.Y-old.Y)*16) / 2
	if dots > 1+1e-8 {
		t.Fatal("resize teleported fish", dots)
	}
}

func TestKoiMotionWholeSidebarAndTextRemain(t *testing.T) {
	in := koiFixture(t, 60)
	in.KoiKitty = true
	in.KoiCellW, in.KoiCellH = 8, 16
	plainIn := in
	plainIn.Koi = false
	plain := Compose(plainIn).Frame
	frame := Compose(in).Frame
	area := in.State.RightSidebarArea()
	if frame.KoiScene.Pond.Y != area.Y || frame.KoiScene.Pond.H != area.H {
		t.Fatal("list still shrinks pond", frame.KoiScene.Pond, area)
	}
	if len(frame.KoiScene.Fish) != KoiCount(60, area.W-1, area.H) {
		t.Fatal("population shrank with list")
	}
	for i, c := range plain.Cells {
		if c.Content != frame.Cells[i].Content {
			t.Fatal("list text changed", i)
		}
	}
	for _, g := range frame.Graphics {
		if g.Koi && g.Placement.Z >= 0 {
			t.Fatal("fish covers text", g.Placement.Z)
		}
	}
	for _, row := range SidebarViewForInput(in).Rows {
		if row.Right && row.Y >= frame.KoiScene.Pond.Y && row.Y < frame.KoiScene.ClickPond.Y {
			var clock KoiAnimation
			if clock.Click(frame.KoiScene, true, area.X+2, row.Y, time.Now()) {
				t.Fatal("fish stole list click")
			}
		}
	}
}

func TestKoiMotionMiniOnlyOnBands(t *testing.T) {
	in := koiFixture(t, 1)
	in.Panes[1].Status.Key = "working"
	frame := Compose(in).Frame
	if len(frame.KoiScene.Mini) == 0 {
		t.Fatal("working workspace has no band fish")
	}
	for _, fish := range frame.KoiScene.Mini {
		found := false
		for _, row := range SidebarViewForInput(in).Rows {
			if !row.Right && row.Y == fish.Rect.Y && row.Row.Kind == RowSection {
				found = true
			}
		}
		if !found {
			t.Fatal("mini on workspace row", fish.Rect)
		}
	}
}
