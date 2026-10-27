package render

import (
	"bytes"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/layout"
	"image"
	"image/color"
	"reflect"
	"strings"
	"testing"
	"time"
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

func TestKoiCompletionUsesProvidedFossilExpiry(t *testing.T) {
	in := koiFixture(t, 1)
	in.Panes[1].Status.Key = "done"
	now := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	var animation KoiAnimation
	animation.Apply(&in, nil, now, 15*time.Second)
	if !in.KoiGlow["local:1"] {
		t.Fatal("completion did not glow")
	}
	animation.Apply(&in, nil, now.Add(15*time.Second-time.Nanosecond), 15*time.Second)
	if !in.KoiGlow["local:1"] {
		t.Fatal("glow ended early")
	}
	animation.Apply(&in, nil, now.Add(15*time.Second), 15*time.Second)
	if in.KoiGlow["local:1"] {
		t.Fatal("glow exceeds fossil expiry")
	}
	in.Panes[1].Status.Key = "working"
	animation.Apply(&in, nil, now.Add(16*time.Second), 15*time.Second)
	in.Panes[1].Status.Key = "done"
	animation.Apply(&in, nil, now.Add(17*time.Second), 15*time.Second)
	if !in.KoiGlow["local:1"] {
		t.Fatal("second completion did not glow")
	}
}

func TestKoiPixelPaletteAndIntegerEnlargement(t *testing.T) {
	for variety := 0; variety < len(KoiPatterns)*3; variety++ {
		for direction := 0; direction < 2; direction++ {
			for wag := 0; wag < 8; wag++ {
				for _, size := range []image.Point{{40, 16}, {24, 8}} {
					small := KoiPixelSprite(variety, direction, wag, size.X, size.Y)
					large := KoiSprite(variety, direction, wag, size.X*2, size.Y*2)
					colors := map[color.RGBA]bool{}
					for y := 0; y < size.Y; y++ {
						for x := 0; x < size.X; x++ {
							c := small.RGBAAt(x, y)
							if c.A != 0 {
								colors[c] = true
								if c.A != 255 {
									t.Fatal("translucent pixel")
								}
							}
							for dy := 0; dy < 2; dy++ {
								for dx := 0; dx < 2; dx++ {
									if large.RGBAAt(x*2+dx, y*2+dy) != c {
										t.Fatal("interpolated enlargement", variety, size, x, y)
									}
								}
							}
						}
					}
					if len(colors) > 8 {
						t.Fatal("palette exceeds eight colors", len(colors))
					}
				}
			}
		}
	}
}

func TestKoiFinalMachineOutputAndAllThemes(t *testing.T) {
	for _, theme := range config.ThemeNames {
		for _, server := range []bool{false, true} {
			for _, transparent := range []bool{false, true} {
				in := koiFixture(t, 2)
				in.ServerUI = server
				in.SidebarConfig.Theme.Name = theme
				in.SidebarConfig.UI.TransparentSidebarCards = transparent
				in.State.Workspaces[0].Name = "鯉"
				in.Panes[1].Status.Key = "working"
				other := in
				machines := &MachineSidebar{Groups: []MachineGroup{{ID: "local", Input: &in}, {ID: "remote", Input: &other}}, Active: "local"}
				in.Koi = false
				plain := ComposeMachines(in, machines)
				in.Koi = true
				scene := KoiSceneFor(plain.Frame, in, machines)
				if len(scene.Fish) != 4 || len(scene.Mini) != 2 {
					t.Fatalf("missing fish %s %t %t pond=%d mini=%d", theme, server, transparent, len(scene.Fish), len(scene.Mini))
				}
				painted := ComposeMachines(in, machines)
				for _, mini := range scene.Mini {
					changed := 0
					for x := mini.Rect.X; x < mini.Rect.X+mini.Rect.W; x++ {
						if !reflect.DeepEqual(plain.Frame.Cell(x, mini.Rect.Y), painted.Frame.Cell(x, mini.Rect.Y)) {
							changed++
						}
					}
					if changed == 0 {
						t.Fatal("machine mini erased", theme, server, transparent)
					}
				}
				pondChanged := 0
				for y := scene.Pond.Y; y < scene.Pond.Y+scene.Pond.H; y++ {
					for x := scene.Pond.X; x < scene.Pond.X+scene.Pond.W; x++ {
						if !reflect.DeepEqual(plain.Frame.Cell(x, y), painted.Frame.Cell(x, y)) {
							pondChanged++
						}
					}
				}
				if pondChanged == 0 {
					t.Fatal("machine pond erased", theme, server, transparent)
				}
				in.KoiKitty = true
				in.KoiCellW, in.KoiCellH = 8, 16
				graphic := ComposeMachines(in, machines)
				if len(graphic.Frame.Graphics) != 7 {
					t.Fatal("missing final machine graphics", len(graphic.Frame.Graphics))
				}
			}
		}
	}
}

func TestKoiOctantsKeepSourcePalette(t *testing.T) {
	for variety := range KoiPatterns {
		fish := KoiFish{Agent: KoiAgent{Seed: variety}, Rect: layout.Rect{W: 10, H: 2}}
		source := KoiSprite(variety, 0, 0, 20, 8)
		frame := NewFrame(10, 2)
		colors := map[color.RGBA]bool{color.RGBAModel.Convert(frame.Palette.PanelBG).(color.RGBA): true}
		for y := 0; y < 8; y++ {
			for x := 0; x < 20; x++ {
				c := source.RGBAAt(x, y)
				if c.A != 0 {
					colors[c] = true
				}
			}
		}
		drawKoiOctants(frame, fish)
		for _, cell := range frame.Cells {
			for _, c := range []color.Color{cell.Style.Fg, cell.Style.Bg} {
				if c != nil && !colors[color.RGBAModel.Convert(c).(color.RGBA)] {
					t.Fatal("octant introduced an interpolated color", variety, c)
				}
			}
		}
	}
}

func TestKoiMiniPlacementUsesEnlargedPixelGrid(t *testing.T) {
	in := koiFixture(t, 1)
	in.Panes[1].Status.Key = "working"
	in.State.Workspaces[0].Name = "鯉"
	in.KoiKitty = true
	in.KoiCellW, in.KoiCellH = 8, 16
	miniFrames := 0
	for tick := 0; tick < 100; tick++ {
		in.KoiTime = float64(tick) / 20
		out := Compose(in)
		for _, g := range out.Frame.Graphics {
			if g.Pane != -2 {
				continue
			}
			miniFrames++
			p := g.Placement
			// The 24x8 source is doubled to 48x16 in this one-row terminal box.
			if (p.X*8+p.OffsetX)%2 != 0 || (p.Y*16+p.OffsetY)%2 != 0 {
				t.Fatalf("mini moved by less than one source dot: tick=%d x=%d y=%d", tick, p.X*8+p.OffsetX, p.Y*16+p.OffsetY)
			}
		}
	}
	if miniFrames != 100 {
		t.Fatal("mini was not rendered for every sampled frame", miniFrames)
	}
}

func TestKoiMiniImageStaysWithinItsTextRowAtFractionalCellScale(t *testing.T) {
	for _, cell := range []image.Point{{8, 16}, {9, 17}, {12, 25}, {16, 32}} {
		lane := layout.Rect{X: 3, Y: 1, W: 20, H: 1}
		fish := koiPose(KoiAgent{Seed: 1, State: "working"}, lane, 6, 0)
		frame := NewFrame(80, 20)
		drawKoiGraphics(frame, KoiScene{Mini: []KoiFish{fish}}, Input{KoiCellW: cell.X, KoiCellH: cell.Y})
		if len(frame.Graphics) != 1 {
			t.Fatal("mini missing")
		}
		p := frame.Graphics[0].Placement
		if p.Y != lane.Y || p.OffsetY != 0 || p.Rows != 1 {
			t.Fatalf("mini crosses text-row boundary: cell=%v row=%d offset=%d height=%d", cell, p.Y, p.OffsetY, p.Rows)
		}
	}
}
