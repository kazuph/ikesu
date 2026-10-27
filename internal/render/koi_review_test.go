package render

import (
	"bytes"
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/creack/pty"
	"github.com/kazuph/ikesu/internal/kitty"
	"github.com/kazuph/ikesu/internal/layout"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKoiGIFContainsEveryFish(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "brand", "koi.gif"))
	if err != nil {
		t.Fatal(err)
	}
	animation, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	for tick, frame := range animation.Image {
		for i := 0; i < 4; i++ {
			fish := koiPose(KoiAgent{Seed: i, State: "idle"}, layout.Rect{X: 0, Y: i * 3, W: 40, H: 2}, 10, float64(tick)/20)
			at := koiPixelPosition(fish, 8, 16)
			box := image.Rectangle{Min: at, Max: at.Add(image.Pt(80, 32))}
			if !box.In(frame.Bounds()) {
				t.Fatalf("frame %d fish %d clipped: %v outside %v", tick, i, box, frame.Bounds())
			}
		}
	}
}

func TestKoiOctantIdleActuallySinks(t *testing.T) {
	centroid := func(state string) float64 {
		f := NewFrame(40, 8)
		for y := 0; y < f.H; y++ {
			for x := 0; x < f.W; x++ {
				f.Set(x, y, " ", uv.Style{Bg: waterBG})
			}
		}
		fish := koiPose(KoiAgent{Seed: 0, State: state}, layout.Rect{X: 1, Y: 1, W: 30, H: 2}, 10, 0)
		drawKoiOctants(f, fish)
		total, n := 0, 0
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
						total += y*4 + i/2
						n++
					}
				}
			}
		}
		if n == 0 {
			t.Fatal("no fish pixels")
		}
		return float64(total) / float64(n)
	}
	working, idle, blocked := centroid("working"), centroid("idle"), centroid("blocked")
	t.Logf("final octant dot centroids: working=%v idle=%v blocked=%v", working, idle, blocked)
	if idle <= working || idle <= blocked {
		t.Fatalf("idle did not sink in final cells: working=%v idle=%v blocked=%v", working, idle, blocked)
	}
}

func TestKoiHeadHasBluntPixelFace(t *testing.T) {
	for _, size := range []image.Point{{40, 16}, {24, 8}} {
		for v := range KoiPatterns {
			img := KoiPixelSprite(v, 0, 0, size.X, size.Y)
			front := -1
			count := 0
			for x := size.X - 1; x >= 0; x-- {
				for y := 0; y < size.Y; y++ {
					if img.RGBAAt(x, y).A != 0 {
						count++
					}
				}
				if count > 0 {
					front = x
					break
				}
			}
			if count < size.Y/4 {
				t.Errorf("variety %d size %v pointed face: x=%d height=%d", v, size, front, count)
			}
		}
	}
}

func TestExportKoiShapeProof(t *testing.T) {
	dest := os.Getenv("KOI_SHAPE_PROOF")
	if dest == "" {
		t.Skip("explicit shape proof export")
	}
	if err := os.MkdirAll(dest, 0755); err != nil {
		t.Fatal(err)
	}
	for _, size := range []image.Point{{40, 16}, {24, 8}} {
		for _, scale := range []int{1, 4} {
			sheet := image.NewRGBA(image.Rect(0, 0, size.X*scale*len(KoiPatterns), size.Y*scale))
			for v := range KoiPatterns {
				sprite := KoiSprite(v, 0, 0, size.X*scale, size.Y*scale)
				at := image.Pt(v*size.X*scale, 0)
				draw.Draw(sheet, sprite.Bounds().Add(at), sprite, image.Point{}, draw.Src)
			}
			name := fmt.Sprintf("koi-%dx%d-%dx.png", size.X, size.Y, scale)
			save := func(name string, img image.Image) {
				f, err := os.Create(filepath.Join(dest, name))
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				if err := png.Encode(f, img); err != nil {
					t.Fatal(err)
				}
			}
			save(name, sheet)
			if before := os.Getenv("KOI_SHAPE_BEFORE"); before != "" {
				f, err := os.Open(filepath.Join(before, name))
				if err != nil {
					t.Fatal(err)
				}
				old, err := png.Decode(f)
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
				comparison := image.NewRGBA(image.Rect(0, 0, sheet.Bounds().Dx(), sheet.Bounds().Dy()*2))
				draw.Draw(comparison, old.Bounds(), old, image.Point{}, draw.Src)
				draw.Draw(comparison, sheet.Bounds().Add(image.Pt(0, sheet.Bounds().Dy())), sheet, image.Point{}, draw.Src)
				save("comparison-"+name, comparison)
			}
		}
	}
}

func TestKoiSSHUsesInlineImages(t *testing.T) {
	for _, key := range []string{"TERM_PROGRAM", "TERM", "KITTY_WINDOW_ID", "SSH_CONNECTION", "SSH_TTY", "TMUX", "STY"} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
	master, terminal, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer terminal.Close()
	t.Setenv("TERM", "xterm-ghostty")
	t.Setenv("SSH_CONNECTION", "192.0.2.1 1 192.0.2.2 2")
	in := koiFixture(t, 1)
	in.KoiKitty = kitty.SupportsImages(terminal, terminal)
	in.KoiCellW, in.KoiCellH = 8, 16
	var enc Encoder
	out := Compose(in)
	wire := string(enc.Frame(out.Frame, 0, 0, false))
	if !strings.Contains(wire, "a=t,t=d,") || strings.Contains(wire, "t=f,") {
		t.Fatal("SSH koi did not use in-band image data")
	}
	t.Setenv("TMUX", "session")
	in.KoiKitty = kitty.SupportsImages(terminal, terminal)
	out = Compose(in)
	if len(out.Frame.Graphics) != 0 || !out.Frame.KoiActive {
		t.Fatal("TMUX did not render octants")
	}
}
