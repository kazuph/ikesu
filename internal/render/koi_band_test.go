package render

import (
	"math"
	"testing"

	"github.com/kazuph/ikesu/internal/layout"
)

func TestKoiBandHorizontalConstantTraverse(t *testing.T) {
	for _, width := range []int{22, 30, 46} {
		for _, state := range []string{"working", "idle", "done"} {
			in := Input{KoiCellW: 8, KoiCellH: 16, KoiMotion: map[string]koiMotion{}}
			lane := layout.Rect{X: 2, Y: 4, W: width, H: 1}
			agent := KoiAgent{ID: "mini:test", Seed: 1, State: state}
			// Start at the left edge to measure a complete visible traverse.
			in.KoiMotion[agent.ID] = koiMotion{Group: "band", X: 2, Y: 4, Direction: 0, Agent: agent, Pond: lane}
			previous := KoiFish{X: 2, Y: 4}
			pace := 1.0
			if state != "working" {
				pace = 0.18
			}
			// Three seconds for ordinary bands, constrained by one source dot/tick.
			_, _, scale := koiPixelGrid(6*8, 16)
			individualPace := (0.18 + koiHash(agent.Seed+11)*0.12) / 0.30
			expected := math.Max(3, float64((width-6)*8)*KoiInterval.Seconds()/float64(scale)) / (pace * individualPace)
			reached := false
			for tick := 1; tick <= int(math.Ceil(expected/KoiInterval.Seconds()))+2; tick++ {
				in.KoiTime = float64(tick) * KoiInterval.Seconds()
				fish := koiResidentsFor([]KoiAgent{agent}, 1, lane, in, "band", 6, 1)[0]
				if fish.Direction != 0 && fish.Direction != 1 {
					t.Fatalf("width=%d state=%s: diagonal direction %d", width, state, fish.Direction)
				}
				if fish.Y != 4 {
					t.Fatal("left band row", fish.Y)
				}
				dots := math.Hypot((fish.X-previous.X)*8, (fish.Y-previous.Y)*16) / float64(scale)
				if dots > 1+1e-8 {
					t.Fatal("over one source dot", dots)
				}
				if fish.X > previous.X && fish.Direction != 0 || fish.X < previous.X && fish.Direction != 1 {
					t.Fatal("heading disagrees with movement")
				}
				if math.Abs(fish.X-float64(lane.X+lane.W-6)) < 1e-8 {
					if math.Abs(in.KoiTime-expected) > KoiInterval.Seconds()+1e-8 {
						t.Fatalf("traverse %.3fs want %.3fs", in.KoiTime, expected)
					}
					t.Logf("width=%d state=%s traverse=%.3fs", width, state, in.KoiTime)
					reached = true
					break
				}
				previous = fish
			}
			if !reached {
				t.Fatalf("width=%d state=%s: did not cross in %.3fs", width, state, expected)
			}
			previous = koiResidentsFor([]KoiAgent{agent}, 1, lane, in, "band", 6, 1)[0]
			in.KoiTime += KoiInterval.Seconds()
			fish := koiResidentsFor([]KoiAgent{agent}, 1, lane, in, "band", 6, 1)[0]
			if fish.Direction != 1 || fish.X >= previous.X {
				t.Fatal("did not reverse immediately", fish)
			}
		}
	}
}

func TestKoiBandLifecycleResizeAndPixelLimit(t *testing.T) {
	in := Input{KoiCellW: 8, KoiCellH: 16, KoiMotion: map[string]koiMotion{}}
	lane := layout.Rect{X: 2, Y: 4, W: 30, H: 1}
	first := KoiAgent{ID: "mini:first", State: "working"}
	waiting := KoiAgent{ID: "mini:waiting", State: "working"}
	fish := koiResidentsFor([]KoiAgent{first}, 1, lane, in, "band", 6, 1)[0]
	if fish.X != float64(lane.X+lane.W) {
		t.Fatal("entry did not start outside edge")
	}
	for tick := 1; tick <= 400; tick++ {
		old := fish
		in.KoiTime = float64(tick) * KoiInterval.Seconds()
		if tick == 180 {
			lane.W = 22
		}
		if tick == 200 {
			lane.W = 38
		}
		scene := koiResidentsFor([]KoiAgent{waiting, first}, 1, lane, in, "band", 6, 1)
		if len(scene) != 1 || scene[0].Agent.ID != first.ID {
			t.Fatal("full band did not wait")
		}
		fish = scene[0]
		_, _, scale := koiPixelGrid(6*8, 16)
		if math.Hypot((fish.X-old.X)*8, (fish.Y-old.Y)*16)/float64(scale) > 1+1e-8 {
			t.Fatal("resize or swimming teleported")
		}
		a, b := koiPixelPosition(old, 8, 16), koiPixelPosition(fish, 8, 16)
		if math.Abs(float64(a.X-b.X))/float64(scale) > 1 {
			t.Fatal("raster moved over one dot")
		}
		if fish.Direction != 0 && fish.Direction != 1 {
			t.Fatal("nonhorizontal direction")
		}
	}
	if in.KoiMotion[first.ID].Entering {
		t.Fatal("entry never finished")
	}
	for tick := 0; tick < 400; tick++ {
		in.KoiTime += KoiInterval.Seconds()
		scene := koiResidentsFor([]KoiAgent{waiting}, 1, lane, in, "band", 6, 1)
		if len(scene) != 1 {
			t.Fatal("departure vanished")
		}
		if scene[0].Agent.ID == waiting.ID {
			if _, ok := in.KoiMotion[first.ID]; ok {
				t.Fatal("departure retained")
			}
			if scene[0].X != float64(lane.X+lane.W) {
				t.Fatal("waiting fish appeared inside")
			}
			return
		}
		if !in.KoiMotion[first.ID].Leaving {
			t.Fatal("missing exit")
		}
	}
	t.Fatal("waiting fish was never admitted")
}

func TestKoiBandBothHeadingsAcrossRoundTrips(t *testing.T) {
	for _, cells := range [][2]int{{8, 16}, {10, 20}, {16, 32}} {
		for seed := 0; seed < 8; seed++ {
			in := Input{KoiCellW: cells[0], KoiCellH: cells[1], KoiMotion: map[string]koiMotion{}}
			lane := layout.Rect{X: 2, Y: 4, W: 30, H: 1}
			agent := KoiAgent{ID: "mini:test", Seed: seed, State: "working"}
			previous := koiResidentsFor([]KoiAgent{agent}, 1, lane, in, "band", 6, 1)[0]
			headings := map[int]bool{}
			for tick := 1; tick <= 1000; tick++ {
				in.KoiTime = float64(tick) * KoiInterval.Seconds()
				fish := koiResidentsFor([]KoiAgent{agent}, 1, lane, in, "band", 6, 1)[0]
				if fish.Direction != 0 && fish.Direction != 1 {
					t.Fatal("not left/right", fish.Direction)
				}
				headings[fish.Direction] = true
				if fish.X > previous.X && fish.Direction != 0 || fish.X < previous.X && fish.Direction != 1 {
					t.Fatal("reversal did not switch heading")
				}
				_, _, scale := koiPixelGrid(6*cells[0], cells[1])
				if math.Hypot((fish.X-previous.X)*float64(cells[0]), (fish.Y-previous.Y)*float64(cells[1]))/float64(scale) > 1+1e-8 {
					t.Fatal("over source dot limit")
				}
				previous = fish
			}
			if len(headings) != 2 {
				t.Fatal("did not swim both directions")
			}
		}
	}
}

func TestKoiBandSimultaneousEntrantsSeparate(t *testing.T) {
	in := Input{KoiCellW: 8, KoiCellH: 16, KoiMotion: map[string]koiMotion{}}
	lane := layout.Rect{X: 2, Y: 4, W: 30, H: 1}
	agents := []KoiAgent{{ID: "mini:first", Seed: 1, State: "working"}, {ID: "mini:second", Seed: 2, State: "working"}}
	var scene []KoiFish
	for tick := 0; tick <= 240; tick++ {
		in.KoiTime = float64(tick) * KoiInterval.Seconds()
		scene = koiResidentsFor(agents, 2, lane, in, "band", 6, 1)
	}
	if len(scene) != 2 || math.Abs(scene[0].X-scene[1].X)*8 < 1 {
		t.Fatal("simultaneous entrants remain stacked", scene)
	}
}
