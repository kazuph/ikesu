package render

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/model"
)

func koiCrowdedFixture(t *testing.T) Input {
	in := koiFixture(t, 1)
	in.State.Rows = 30
	in.State.Workspaces[0].Section = model.SectionWork
	for i := 1; i < 12; i++ {
		in.State.Apply(model.CmdNewTab{Workspace: 0, NoFocus: true})
	}
	for _, tab := range in.State.Workspaces[0].Tabs {
		for _, id := range tab.Layout.PaneIDs() {
			in.Panes[id] = &PaneSnap{Agent: "codex", TaskTitle: fmt.Sprintf("task %02d", id), Status: StatusGlyph{Key: "working", Rune: "*", Color: Yellow}}
		}
	}
	in.KoiKitty = true
	in.KoiCellW, in.KoiCellH = 8, 16
	return in
}

func TestKoiMotionCrowdedSidebarProof(t *testing.T) {
	in := koiCrowdedFixture(t)
	plainIn := in
	plainIn.Koi = false
	plain := Compose(plainIn).Frame
	var clock KoiAnimation
	now := time.Unix(100, 0)
	clock.Apply(&in, nil, now, 15*time.Second)
	Compose(in)
	for tick := 1; tick <= 120; tick++ {
		clock.Apply(&in, nil, now.Add(time.Duration(tick)*KoiInterval), 15*time.Second)
		Compose(in)
	}
	frame := Compose(in).Frame
	if !reflect.DeepEqual(plain.Cells, frame.Cells) {
		t.Fatal("fish changed text or backgrounds")
	}
	if len(frame.KoiScene.Fish) != KoiCount(12, frame.KoiScene.Pond.W, frame.KoiScene.Pond.H) {
		t.Fatal("crowded list reduced capacity")
	}
	overlap := 0
	for _, g := range frame.Graphics {
		if g.Placement.Z >= 0 {
			t.Fatal("fish is in front of text")
		}
		p := g.Placement
		for y := p.Y; y < p.Y+p.Rows; y++ {
			for x := p.X; x < p.X+p.Cols; x++ {
				if c := plain.Cell(x, y); c != nil && strings.TrimSpace(c.Content) != "" {
					overlap++
				}
			}
		}
	}
	if overlap == 0 {
		t.Fatal("fixture did not put fish behind list text")
	}
	t.Logf("real pane count=%d resident fish=%d text cells under sprites=%d unchanged cells=%d", len(in.Panes), len(frame.KoiScene.Fish), overlap, len(frame.Cells))
	dest := os.Getenv("KOI_MOTION_EXPORT")
	if dest == "" {
		return
	}
	if err := os.MkdirAll(dest, 0755); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"plain", "fish", "mask"} {
		var b strings.Builder
		fmt.Fprintf(&b, `<!doctype html><meta charset="utf-8"><title>Compose terminal frame %s</title><style>body{margin:0;background:#111}#frame{position:relative;isolation:isolate;width:%dpx;height:%dpx}span{position:absolute;font:13px/16px Menlo,monospace;white-space:pre}img{position:absolute;image-rendering:pixelated}</style><div id="frame">`, kind, frame.W*8, frame.H*16)
		if kind != "mask" {
			for y := 0; y < frame.H; y++ {
				for x := 0; x < frame.W; x++ {
					c := frame.Cell(x, y)
					fmt.Fprintf(&b, `<span style="left:%dpx;top:%dpx;width:8px;height:16px;background:%s"></span>`, x*8, y*16, cssColor(c.Style.Bg, "#1e1e2e"))
				}
			}
		}
		if kind == "fish" {
			for _, g := range frame.Graphics {
				p := g.Placement
				width, height := p.Cols*8, p.Rows*16
				if width == 0 {
					width = g.Source.Dx()
				}
				if height == 0 {
					height = g.Source.Dy()
				}
				sx, sy := 0, 0
				if !g.Source.Empty() {
					sx, sy = g.Source.Min.X, g.Source.Min.Y
				}
				fmt.Fprintf(&b, `<div class="sprite" style="position:absolute;left:%dpx;top:%dpx;width:%dpx;height:%dpx;overflow:hidden;z-index:%d"><img style="left:%dpx;top:%dpx;width:%dpx;height:%dpx" src="data:image/png;base64,%s"></div>`, p.X*8+p.OffsetX, p.Y*16+p.OffsetY, width, height, 1000+p.Z, -sx, -sy, p.Image.Width, p.Image.Height, base64.StdEncoding.EncodeToString(p.Image.Data))
			}
		}
		for y := 0; y < frame.H; y++ {
			for x := 0; x < frame.W; x++ {
				c := frame.Cell(x, y)
				if c.Width == 0 {
					continue
				}
				fg := cssColor(c.Style.Fg, "#cdd6f4")
				if kind == "mask" {
					fg = "#ffffff"
				}
				fmt.Fprintf(&b, `<span class="glyph" style="left:%dpx;top:%dpx;width:%dpx;height:16px;color:%s;z-index:2000">%s</span>`, x*8, y*16, c.Width*8, fg, html.EscapeString(c.Content))
			}
		}
		b.WriteString("</div>")
		if err := os.WriteFile(filepath.Join(dest, kind+".html"), []byte(b.String()), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExportKoiMotionSpriteManifest(t *testing.T) {
	dest := os.Getenv("KOI_MOTION_MANIFEST")
	if dest == "" {
		return
	}
	type sprite struct{ Variety, Direction, Wag, W, H int }
	manifest := map[string][]sprite{}
	for _, size := range [][2]int{{80, 80}, {48, 16}} {
		directions := 16
		varieties := 4 * len(KoiPatterns)
		if size[1] == 16 {
			directions = 2
			varieties = len(KoiPatterns)
		}
		for v := 0; v < varieties; v++ {
			for d := 0; d < directions; d++ {
				for wag := 0; wag < 8; wag++ {
					img, _ := koiSpriteCached(koiSpriteKey{v, d, wag, size[0], size[1]})
					sum := sha256.Sum256(img.Pix)
					key := fmt.Sprintf("%x", sum)
					manifest[key] = append(manifest[key], sprite{v, d, wag, size[0], size[1]})
				}
			}
		}
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(dest, data, 0644); err != nil {
		t.Fatal(err)
	}
	t.Logf("unique source sprites=%d", len(manifest))
}
