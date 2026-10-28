package render

import (
	"math"
	"testing"

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
