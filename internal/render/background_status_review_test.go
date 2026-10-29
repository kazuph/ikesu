package render

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/model"
	"image/color"
	"strings"
	"testing"
)

func TestModeAndCopyStatusUseCommonBackground(t *testing.T) {
	for _, theme := range []string{"catppuccin", "catppuccin-latte"} {
		for _, mode := range []string{"default", "surface_dim", "transparent"} {
			for _, view := range []string{"resize", "navigate", "copy", "fullscreen-copy", "legacy-copy", "legacy-fullscreen-copy"} {
				t.Run(theme+"/"+mode+"/"+view, func(t *testing.T) {
					s := model.New(t.TempDir(), 120, 40)
					c := config.Config{}
					c.Theme.Name = theme
					var want color.Color
					if mode == "surface_dim" {
						c.Theme.Custom = map[string]string{"surface_dim": "#234567"}
						want = color.RGBA{35, 69, 103, 255}
					}
					if mode == "transparent" {
						c.UI.OpaqueBackground = true
						c.Theme.Custom = map[string]string{"panel_bg": "transparent"}
					}
					in := Input{State: s, SidebarConfig: c, ServerUI: !strings.HasPrefix(view, "legacy")}
					if view == "resize" {
						in.ModeHint = " RESIZE  h/l width  j/k height  esc exit "
					}
					if view == "navigate" {
						in.ModeHint = " NAVIGATE  up / down select  esc exit "
					}
					if strings.Contains(view, "copy") {
						in.Copy = &CopyMark{Pane: 1, Fullscreen: strings.Contains(view, "fullscreen")}
						in.CopyHint = " COPY  /find█"
					}
					f := Compose(in).Frame
					area := s.PanesArea()
					y := area.Y + area.H - 1
					startX := area.X
					if in.Copy != nil && in.ServerUI {
						y = s.Rows - 1
						startX = 0
					}
					text := ""
					for x := startX; x < f.W; x++ {
						cell := f.Cell(x, y)
						if cell.Style.Bg != want {
							t.Fatalf("status cell %d,%d background=%v want=%v", x, y, cell.Style.Bg, want)
						}
						text += cell.Content
					}
					label := "NAVIGATE"
					if view == "resize" {
						label = "RESIZE"
					}
					if in.Copy != nil {
						label = "COPY"
						if !strings.Contains(text, "/find█") {
							t.Fatalf("search/cursor lost: %q", text)
						}
					}
					if !strings.Contains(text, label) {
						t.Fatalf("mode label lost: %q", text)
					}
					labelColor := f.Palette.Accent
					if view == "resize" {
						labelColor = f.Palette.Mauve
					}
					cell := f.Cell(startX+1, y)
					if cell.Style.Attrs&uv.AttrBold == 0 || cell.Style.Underline != uv.UnderlineSingle || cell.Style.Fg != labelColor {
						t.Fatal("mode label lost its color or bold underline")
					}
				})
			}
		}
	}
}
