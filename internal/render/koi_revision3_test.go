package render

import (
	"github.com/kazuph/ikesu/internal/kitty"
	"github.com/kazuph/ikesu/internal/layout"
	"image/color"
	"math"
	"testing"
	"time"
)

func TestKoiRev3ServerUISourcesHaveSingleOwner(t *testing.T) {
	in := koiFixture(t, 2)
	in.ServerUI = true
	in.KoiKitty = true
	in.KoiCellW = 8
	in.KoiCellH = 16
	pid := in.State.FocusedPane()
	in.Panes[pid].Graphics = []kitty.Placement{{Image: kitty.Image{ID: 99, Format: 32, Width: 1, Height: 1, Data: []byte{255, 0, 0, 255}}, X: 2, Y: 2, Cols: 2, Rows: 2}}
	out := Compose(in)
	regular, koi := withoutKoi(out.Frame)
	if len(koi) == 0 || len(regular.Graphics) == 0 {
		t.Fatal("fixture must exercise simultaneous koi and pane images")
	}
	for _, g := range regular.GraphicsSources {
		if g.Koi {
			t.Fatal("koi is also owned by ordinary image sender")
		}
	}
}

func octantCentroid(f *Frame) (float64, float64) {
	sx, sy, n := 0., 0., 0.
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			c := f.Cell(x, y)
			mask := -1
			for i, r := range octantRunes {
				if c.Content == string(r) {
					mask = i
					break
				}
			}
			if mask < 0 {
				continue
			}
			for i := 0; i < 8; i++ {
				v := c.Style.Bg
				if mask&(1<<i) != 0 {
					v = c.Style.Fg
				}
				if v != nil && color.RGBAModel.Convert(v) != waterBG {
					sx += float64(x*2 + i%2)
					sy += float64(y*4 + i/2)
					n++
				}
			}
		}
	}
	return sx / n, sy / n
}
func TestKoiRev3OctantsMoveOneDotAcrossCellBoundary(t *testing.T) {
	pose := func(x, y float64) (float64, float64) {
		f := NewFrame(40, 30)
		for i := range f.Cells {
			f.Cells[i].Style.Bg = waterBG
		}
		fish := KoiFish{Agent: KoiAgent{Seed: 0}, Rect: layout.Rect{X: int(x), Y: int(y), W: 10, H: 5}, Lane: layout.Rect{W: 40, H: 30}, X: x, Y: y}
		drawKoiOctants(f, fish)
		return octantCentroid(f)
	}
	ax, ay := pose(9.5, 10.75)
	bx, by := pose(10, 10.75)
	cx, cy := pose(9.5, 11)
	if math.Abs((bx-ax)-1) > .5 || math.Abs(by-ay) > .5 {
		t.Fatalf("horizontal subcell lost: %.3f,%.3f", bx-ax, by-ay)
	}
	if math.Abs((cy-ay)-1) > .5 || math.Abs(cx-ax) > .5 {
		t.Fatalf("vertical subcell lost: %.3f,%.3f", cx-ax, cy-ay)
	}
}

func TestKoiRev3FreeSwimIsTwiceRevision2(t *testing.T) {
	pond := layout.Rect{W: 30, H: 40}
	rx, ry := float64(pond.W-10)/2, float64(pond.H-5)/2
	oldSpeed := 1 / (.05 * math.Hypot(rx*8, ry*16) / 2)
	period := 2 * math.Pi / oldSpeed
	newDistance, oldDistance := 0., 0.
	for seed := 1; seed <= 5; seed++ {
		oldPose := func(t float64) (float64, float64) {
			return rx * (1 + math.Sin(t*oldSpeed+koiHash(seed+29)*2*math.Pi)), ry * (1 + math.Sin(t*oldSpeed+koiHash(seed+71)*2*math.Pi))
		}
		agent := KoiAgent{Seed: seed, State: "working"}
		previous := koiPondPose(agent, pond, Input{})
		ox, oy := oldPose(0)
		for sample := 1; sample <= 2000; sample++ {
			at := period * float64(sample) / 2000
			next := koiPondPose(agent, pond, Input{KoiTime: at})
			x, y := oldPose(at)
			newDistance += math.Hypot((next.X-previous.X)*8, (next.Y-previous.Y)*16)
			oldDistance += math.Hypot((x-ox)*8, (y-oy)*16)
			previous = next
			ox, oy = x, y
		}
	}
	ratio := newDistance / oldDistance
	t.Logf("revision2 distance=%.3f new=%.3f ratio=%.6f", oldDistance, newDistance, ratio)
	if math.Abs(ratio-2) > .01 {
		t.Fatalf("free swim is not twice the approved baseline: %.6f", ratio)
	}
	if KoiInterval != 25*time.Millisecond {
		t.Fatal("moving koi requires 25ms samples", KoiInterval)
	}
}

func TestKoiRev3RippleAndGatherRespondFaster(t *testing.T) {
	in := koiFixture(t, 5)
	for _, p := range in.Panes {
		p.Status.Key = "working"
	}
	in.KoiKitty = true
	in.KoiCellW = 8
	in.KoiCellH = 16
	now := time.Now()
	var clock KoiAnimation
	clock.Apply(&in, nil, now, 15*time.Second)
	scene := Compose(in).Frame.KoiScene
	if !clock.Click(scene, true, scene.Pond.X+scene.Pond.W/2, scene.Pond.Y+scene.Pond.H/2, now) {
		t.Fatal("click rejected")
	}
	clock.Apply(&in, nil, now, 15*time.Second)
	if _, ok := koiRippleFish(AnimateKoi(scene, in).KoiScene); !ok {
		t.Fatal("first click frame contains no ripple")
	}
	clock.Apply(&in, nil, now.Add(800*time.Millisecond), 15*time.Second)
	if _, ok := koiRippleFish(AnimateKoi(scene, in).KoiScene); ok {
		t.Fatal("ripple outlives approved 0.8 seconds")
	}
}

func TestKoiRev3GatherAccelerationIsBoundedAndStopsSmoothly(t *testing.T) {
	in := koiFixture(t, 5)
	for _, p := range in.Panes {
		p.Status.Key = "working"
	}
	in.KoiKitty = true
	in.KoiCellW = 8
	in.KoiCellH = 16
	now := time.Now()
	var clock KoiAnimation
	clock.Apply(&in, nil, now, 15*time.Second)
	scene := Compose(in).Frame.KoiScene
	x, y := scene.Pond.X+scene.Pond.W/2, scene.Pond.Y+scene.Pond.H/2
	distance := func(f KoiFish) float64 { return math.Hypot((f.X+5-float64(x)-.5)*8, (f.Y+2.5-float64(y)-.5)*16) / 2 }
	initial := map[string]float64{}
	for _, f := range scene.Fish {
		initial[f.Agent.ID] = distance(f)
	}
	clock.Click(scene, true, x, y, now)
	clock.Apply(&in, nil, now, 15*time.Second)
	previous := map[string]koiMotion{}
	for id, m := range clock.motion {
		previous[id] = m
	}
	near := 0
	for tick := 1; tick <= int(KoiAttractionLifetime/KoiInterval.Seconds()); tick++ {
		clock.Apply(&in, nil, now.Add(time.Duration(tick)*KoiInterval), 15*time.Second)
		next := AnimateKoi(scene, in).KoiScene
		for _, f := range next.Fish {
			m, old := clock.motion[f.Agent.ID], previous[f.Agent.ID]
			if m.Speed > 1/KoiFreeDotInterval.Seconds()*KoiAttractionSpeedLimit+1e-9 {
				t.Fatal("attraction exceeded approved speed cap")
			}
			acceleration := 1 / KoiFreeDotInterval.Seconds() * KoiAttractionSpeedLimit / KoiGatherDuration
			if math.Abs(m.Speed-old.Speed) > acceleration*KoiInterval.Seconds()+1e-8 {
				t.Fatal("acceleration or braking has a discontinuity", m.Speed, old.Speed)
			}
			if tick == int(KoiGatherDuration/KoiInterval.Seconds()) {
				final := distance(f)
				t.Logf("fish=%s initial=%.3f dots after_1.5s=%.3f dots", f.Agent.ID, initial[f.Agent.ID], final)
				if final >= initial[f.Agent.ID] {
					t.Fatal("fish failed to approach")
				}
				if final <= koiPixelWidth {
					near++
				}
			}
			previous[f.Agent.ID] = m
		}
		scene = next
	}
	t.Logf("within_one_40_dot_body_length_after_1.5s=%d/%d", near, len(scene.Fish))
	if near == 0 {
		t.Fatal("no fish gathered close in the approved interval")
	}
	if in.KoiRipple != nil {
		t.Fatal("attraction does not return to free swimming")
	}
	idle := KoiScene{Fish: []KoiFish{{Agent: KoiAgent{State: "idle"}}}}
	if idle.UpdateInterval() == KoiInterval {
		t.Fatal("25ms update selected for stationary fish")
	}
}
