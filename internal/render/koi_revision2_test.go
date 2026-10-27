package render

import (
	"github.com/kazuph/ikesu/internal/layout"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestKoiRevision2PreservesSidebarBackground(t *testing.T) {
	for _, kitty := range []bool{false, true} {
		in := koiFixture(t, 4)
		in.KoiKitty = kitty
		in.KoiCellW = 8
		in.KoiCellH = 16
		in.Koi = false
		plain := Compose(in)
		in.Koi = true
		out := Compose(in)
		scene := KoiSceneFor(plain.Frame, in, nil)
		if kitty {
			for _, g := range out.Frame.Graphics {
				if g.Pane == -3 {
					t.Fatal("water image still placed")
				}
			}
			if !reflect.DeepEqual(plain.Frame.Cells, out.Frame.Cells) {
				t.Fatal("kitty changes background cells")
			}
		} else {
			for y := scene.Pond.Y; y < scene.Pond.Y+scene.Pond.H; y++ {
				for x := scene.Pond.X; x < scene.Pond.X+scene.Pond.W; x++ {
					a, b := plain.Frame.Cell(x, y), out.Frame.Cell(x, y)
					if b.Content == " " && !reflect.DeepEqual(a, b) {
						t.Fatalf("empty pond cell changed at %d,%d", x, y)
					}
				}
			}
		}
	}
}

func TestKoiRevision2TraversesPond(t *testing.T) {
	in := koiFixture(t, 5)
	for _, p := range in.Panes {
		p.Status.Key = "working"
	}
	f := NewFrame(in.State.Cols, in.State.Rows)
	low, high := false, false
	for tick := 0; tick < 1200; tick++ {
		in.KoiTime = float64(tick)
		scene := KoiSceneFor(f, in, nil)
		for _, fish := range scene.Fish {
			u := (fish.Y - float64(scene.Pond.Y)) / float64(scene.Pond.H-fish.Rect.H)
			low = low || u < .25
			high = high || u > .75
		}
	}
	if !low || !high {
		t.Fatalf("fish remain in top lanes: low=%t high=%t", low, high)
	}
}

func TestKoiRevision2DepthAndStationaryIdle(t *testing.T) {
	in := koiFixture(t, 5)
	in.Panes[1].Status.Key = "blocked"
	in.Panes[2].Status.Key = "working"
	in.KoiKitty = true
	in.KoiCellW = 8
	in.KoiCellH = 16
	out := Compose(in)
	scene := out.Frame.KoiScene
	for i := 1; i < len(scene.Fish); i++ {
		if scene.Fish[i-1].Depth < scene.Fish[i].Depth {
			t.Fatal("incorrect depth order")
		}
	}
	for i, fish := range scene.Fish {
		if fish.Agent.State == "blocked" && fish.Depth != 0 {
			t.Fatal("blocked not at surface")
		}
		if fish.Agent.State == "idle" && fish.Depth < 1 {
			t.Fatal("idle not deep")
		}
		if i > 0 && out.Frame.Graphics[i-1].Placement.Z > out.Frame.Graphics[i].Placement.Z {
			t.Fatal("kitty z disagrees with depth")
		}
	}
	for _, p := range in.Panes {
		p.Status.Key = "idle"
	}
	a := Compose(in)
	in.KoiTime = 100
	b := Compose(in)
	if a.Frame.KoiActive || b.Frame.KoiActive {
		t.Fatal("idle keeps clock active")
	}
	if !reflect.DeepEqual(a.Frame.Graphics, b.Frame.Graphics) {
		t.Fatal("settled idle moves")
	}
}

func TestKoiRevision2SmoothClickAndRelease(t *testing.T) {
	in := koiFixture(t, 5)
	for _, p := range in.Panes {
		p.Status.Key = "working"
	}
	in.KoiKitty = true
	in.KoiCellW = 8
	in.KoiCellH = 16
	now := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	var clock KoiAnimation
	clock.Apply(&in, nil, now, 15*time.Second)
	frame := Compose(in).Frame
	scene := frame.KoiScene
	x, y := scene.Pond.X+scene.Pond.W/2, scene.Pond.Y+scene.Pond.H/2
	if clock.Click(scene.Pond, x, scene.Pond.Y-1, now) {
		t.Fatal("list/header consumed")
	}
	if !clock.Click(scene.Pond, x, y, now) {
		t.Fatal("pond click rejected")
	}
	distance := func(scene KoiScene) float64 {
		var d float64
		for _, f := range scene.Fish {
			d += math.Hypot((f.X+5-float64(x))*8, (f.Y+2.5-float64(y))*16)
		}
		return d
	}
	before := distance(scene)
	for tick := 1; tick <= 120; tick++ {
		clock.Apply(&in, nil, now.Add(time.Duration(tick)*KoiInterval), 15*time.Second)
		next := AnimateKoi(scene, in).KoiScene
		for i, f := range next.Fish {
			a, b := koiPixelPosition(scene.Fish[i], 8, 16), koiPixelPosition(f, 8, 16)
			if math.Abs(float64(a.X-b.X)) > 2 || math.Abs(float64(a.Y-b.Y)) > 2 {
				t.Fatalf("skipped source dot at tick=%d: %v -> %v", tick, a, b)
			}
			turn := (f.Direction-scene.Fish[i].Direction+24)%16 - 8
			if math.Abs(float64(turn)) > 1 {
				t.Fatalf("direction jumps %d sectors", turn)
			}
		}
		scene = next
		if tick == 60 && distance(scene) >= before {
			t.Fatalf("fish did not gather: %.2f -> %.2f", before, distance(scene))
		}
	}
	if in.KoiRipple != nil {
		t.Fatal("click did not release back to free swim")
	}
}

func TestKoiRevision2DemandUploadsNeverRepeat(t *testing.T) {
	in := koiFixture(t, 5)
	for _, p := range in.Panes {
		p.Status.Key = "working"
	}
	in.KoiKitty = true
	in.KoiCellW = 8
	in.KoiCellH = 16
	var enc Encoder
	seen := map[int]bool{}
	pattern := regexp.MustCompile(`a=t,t=d,[^;]*,i=(\d+),`)
	first, uploads, total := 0, 0, 0
	scene := Compose(in).Frame.KoiScene
	for tick := 0; tick <= 12000; tick++ {
		in.KoiTime = float64(tick) * KoiInterval.Seconds()
		frame := AnimateKoi(scene, in)
		data := enc.KoiFrame(frame, 0, 0, false)
		total += len(data)
		matches := pattern.FindAllSubmatch(data, -1)
		if tick == 0 {
			first = len(data)
			if len(matches) > len(scene.Fish)+len(scene.Mini) {
				t.Fatal("unused sprites uploaded initially")
			}
		}
		for _, m := range matches {
			id, _ := strconv.Atoi(string(m[1]))
			if seen[id] {
				t.Fatal("image uploaded twice", id)
			}
			seen[id] = true
			uploads++
		}
	}
	t.Logf("600 seconds first_bytes=%d total_bytes=%d unique_uploads=%d", first, total, uploads)
	// Hiding and returning must reuse the presenter's existing pixels.
	enc.KoiFrame(&Frame{}, 0, 0, false)
	data := enc.KoiFrame(AnimateKoi(scene, in), 0, 0, false)
	if strings.Contains(string(data), "a=t,") {
		t.Fatal("overlay return resends image")
	}
}

func TestKoiRevision2CapacityAcrossPopulationAndArea(t *testing.T) {
	for _, n := range []int{0, 1, 4, 5, 30, 60} {
		for _, h := range []int{3, 10, 80} {
			for _, w := range []int{11, 12, 30, 60} {
				want := min(n, w*h/(10*5*4))
				if w < 12 {
					want = 0
				}
				if got := KoiCount(n, w, h); got != want {
					t.Fatalf("%d %dx%d: %d != %d", n, w, h, got, want)
				}
			}
		}
	}
	pond := layout.Rect{W: 30, H: 80}
	for seed := 1; seed <= 60; seed++ {
		var upper, lower int
		for tick := 0; tick < 1200; tick++ {
			f := koiPondPose(KoiAgent{Seed: seed, State: "working"}, pond, Input{KoiTime: float64(tick)})
			if f.Y < float64(pond.H-f.Rect.H)/2 {
				upper++
			} else {
				lower++
			}
		}
		// Each seed traverses both halves; a full sine orbit has equal residence.
		if math.Abs(float64(upper-lower)) > float64(upper+lower)/4 {
			t.Fatalf("top/bottom bias seed=%d %d/%d", seed, upper, lower)
		}
	}
}
