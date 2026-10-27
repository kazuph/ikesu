package render

import (
	"bytes"
	"github.com/kazuph/ikesu/internal/layout"
	"reflect"
	"strings"
	"testing"
)

func TestKoiCapacityReservesSeparateLanes(t *testing.T) {
	for _, tc := range []struct{ n, w, h, want int }{{0, 30, 3, 0}, {1, 30, 3, 1}, {4, 30, 3, 1}, {5, 30, 10, 3}, {30, 30, 10, 3}, {60, 30, 10, 3}, {30, 11, 10, 0}} {
		if got := KoiCount(tc.n, tc.w, tc.h); got != tc.want {
			t.Errorf("agents=%d pond=%dx%d count=%d want=%d", tc.n, tc.w, tc.h, got, tc.want)
		}
	}
}

func koiFixture(t *testing.T, n int) Input {
	t.Helper()
	in := continuousInput(t, true, false)
	in.State.Cols, in.State.Rows = 160, 100
	in.State.SidebarVisible = true
	in.State.SidebarSize = 26
	in.State.RightSidebarSize = 30
	in.State.RightSidebarMode = ""
	in.Panes = map[int]*PaneSnap{}
	for i := 1; i <= n; i++ {
		in.Panes[i] = &PaneSnap{Agent: "codex", Status: StatusGlyph{Key: "idle"}}
	}
	in.Koi = true
	return in
}

func TestKoiTotalsPriorityAndNonIntersection(t *testing.T) {
	for _, n := range []int{0, 1, 4, 5, 30, 60} {
		for _, height := range []int{3, 10} {
			in := koiFixture(t, n)
			if n > 0 {
				in.Panes[n].Status.Key = "blocked"
			}
			agents := KoiAgentsFor(in, nil)
			if len(agents) != n {
				t.Fatal("lost agent", n, len(agents))
			}
			if n > 0 && agents[0].State != "blocked" {
				t.Fatal("blocked fish thinned out")
			}
			capacity := KoiCount(n, 30, height)
			for tick := 0; tick < 400; tick++ {
				var prev KoiFish
				for i := 0; i < capacity; i++ {
					fish := koiPose(agents[i], layout.Rect{X: 1, Y: 2 + i*3, W: 30, H: 2}, 10, float64(tick)/20)
					if fish.X < 1 || fish.X+float64(fish.Rect.W) > 31 || fish.Y+float64(fish.Rect.H) > float64(2+height) {
						t.Fatal("outside pond", fish)
					}
					if i > 0 && prev.Y+float64(prev.Rect.H) >= fish.Y {
						t.Fatal("intersecting fish", prev, fish)
					}
					prev = fish
				}
			}
			in.AgentScroll = 30
			in.State.Rows = 45
			if !reflect.DeepEqual(agents, KoiAgentsFor(in, nil)) {
				t.Fatal("viewport changed agent population")
			}
		}
	}
}

func TestKoiNonDecorationCellsAndHitsRemainIdentical(t *testing.T) {
	for width := 2; width <= 30; width++ {
		for _, server := range []bool{false, true} {
			for _, transparent := range []bool{false, true} {
				for _, multi := range []bool{false, true} {
					in := koiFixture(t, 2)
					in.ServerUI = server
					in.SidebarConfig.UI.TransparentSidebarCards = transparent
					in.State.SidebarMin = 1
					in.State.SidebarVisible = true
					in.State.SidebarSize = width
					in.State.RightSidebarSize = width
					in.State.Workspaces[0].Name = "鯉🐈é"
					in.Panes[1].Status.Key = "working"
					selected := 0
					in.SelectedWorkspace = &selected
					var groups *MachineSidebar
					if multi {
						remote := in
						groups = &MachineSidebar{Groups: []MachineGroup{{ID: "local", Input: &in}, {ID: "remote", Input: &remote}}, Active: "local"}
					}
					in.Koi = false
					plain := ComposeMachines(in, groups)
					var hits []MachineHit
					if groups != nil {
						hits = append(hits, groups.Hits...)
					}
					in.Koi = true
					out := ComposeMachines(in, groups)
					if groups != nil && !reflect.DeepEqual(hits, groups.Hits) {
						t.Fatal("hit targets changed")
					}
					scene := KoiSceneFor(plain.Frame, in, groups)
					decorated := func(x, y int) bool {
						if len(scene.Fish) > 0 && x >= scene.Pond.X && x < scene.Pond.X+scene.Pond.W && y >= scene.Pond.Y && y < scene.Pond.Y+scene.Pond.H {
							return true
						}
						for _, fish := range scene.Mini {
							r := fish.Rect
							if x >= r.X && x < r.X+r.W && y == r.Y {
								return true
							}
						}
						return false
					}
					for y := 0; y < out.Frame.H; y++ {
						for x := 0; x < out.Frame.W; x++ {
							if !decorated(x, y) && !reflect.DeepEqual(out.Frame.Cell(x, y), plain.Frame.Cell(x, y)) {
								t.Fatalf("cell changed width=%d server=%t transparent=%t multi=%t at %d,%d", width, server, transparent, multi, x, y)
							}
							before := plain.Frame.Cell(x, y)
							if before.Content != " " || before.Width != 1 {
								if !reflect.DeepEqual(before, out.Frame.Cell(x, y)) {
									t.Fatal("text or wide cell overwritten", x, y)
								}
							}
						}
					}
					off := false
					in.SidebarConfig.UI.Koi = &off
					disabled := ComposeMachines(in, groups)
					if !reflect.DeepEqual(disabled.Frame.Cells, plain.Frame.Cells) || len(disabled.Frame.Graphics) != 0 || disabled.Frame.KoiActive {
						t.Fatal("off differs from plain")
					}
				}
			}
		}
	}
}

func TestKoiKittyUploadsOnlyOnceAndOverlaysClear(t *testing.T) {
	in := koiFixture(t, 4)
	in.KoiKitty = true
	in.KoiCellW, in.KoiCellH = 8, 16
	var encoder Encoder
	var first, subsequent int
	for tick := 0; tick < 20; tick++ {
		in.KoiTime = float64(tick) / 20
		out := Compose(in)
		bytes := encoder.Frame(out.Frame, 0, 0, false)
		if tick == 0 {
			first = len(bytes)
			if !strings.Contains(string(bytes), "a=t,") {
				t.Fatal("no upload")
			}
		} else {
			subsequent += len(bytes)
			if strings.Contains(string(bytes), "a=t,") {
				t.Fatal("repeated upload", tick)
			}
		}
		if len(out.Frame.Graphics) < 2 {
			t.Fatal("water and fish missing")
		}
	}
	t.Logf("kitty first=%d bytes subsequent19=%d bytes mean=%.1f", first, subsequent, float64(subsequent)/19)
	in.Menu = &Menu{X: 0, Y: 0, Items: []MenuItem{{Label: "test"}}}
	out := Compose(in)
	if out.Frame.KoiActive || len(out.Frame.Graphics) != 0 {
		t.Fatal("overlay retained koi")
	}
	if b := encoder.Frame(out.Frame, 0, 0, false); !strings.Contains(string(b), "a=d,") {
		t.Fatal("no image cleanup")
	}
}

func TestKoiStateFramesAndOctantPositions(t *testing.T) {
	for mask, cp := range map[int]rune{1: 0x1cea8, 4: 0x1cd00, 15: 0x2580, 85: 0x258c, 170: 0x2590, 255: 0x2588} {
		if octantRunes[mask] != cp {
			t.Fatal("Unicode mapping", mask)
		}
	}
	normal := KoiSprite(0, 0, 0, 80, 32)
	mouth := KoiSprite(5, 0, 0, 80, 32)
	glow := KoiSprite(10, 0, 0, 80, 32)
	if bytes.Equal(normal.Pix, mouth.Pix) || bytes.Equal(normal.Pix, glow.Pix) {
		t.Fatal("state not visible")
	}
	in := koiFixture(t, 1)
	in.Panes[1].Status.Key = "working"
	f := NewFrame(in.State.Cols, in.State.Rows)
	scene := KoiSceneFor(f, in, nil)
	if len(scene.Mini) == 0 {
		t.Fatal("working workspace has no mini")
	}
	in.Panes[1].Status.Key = "blocked"
	if len(KoiSceneFor(f, in, nil).Mini) != 0 {
		t.Fatal("blocked workspace has mini")
	}
}
