package render

import (
	"github.com/kazuph/ikesu/internal/model"
	"image/color"
	"testing"
)

func TestSudoColorIsDistinctInEveryTheme(t *testing.T) {
	for _, name := range ThemeNames {
		p := ThemePalette(name)
		for _, other := range []color.Color{p.Yellow, p.Red, p.Green, p.NavigatorDone, p.Dim, p.Text} {
			if p.Mauve == other {
				t.Errorf("sudo color collides in theme %s", name)
			}
		}
	}
}
func TestNavigatorSudoDiamondUsesDedicatedColor(t *testing.T) {
	s := model.New(t.TempDir(), 120, 50)
	out := Compose(Input{State: s, Navigator: &Navigator{Rows: []NavigatorRow{{Pane: 1, Name: "sudo password wait", Status: "◆"}}, Total: 1}})
	found := false
	for y := 0; y < out.Frame.H; y++ {
		for x := 0; x < out.Frame.W; x++ {
			c := out.Frame.Cell(x, y)
			if c.Content == "◆" {
				found = true
				if c.Style.Fg != out.Frame.Palette.Mauve {
					t.Error("sudo marker did not use dedicated color")
				}
			}
		}
	}
	if !found {
		t.Fatal("sudo marker not rendered")
	}
}

func TestSudoAttentionRankBetweenBlockedAndOtherStates(t *testing.T) {
	sudo := glyphRank(StatusGlyph{Key: "unknown", Rune: "◆"})
	if sudo >= glyphRank(StatusGlyph{Key: "blocked", Rune: "●"}) {
		t.Fatal("sudo overrode blocked")
	}
	for _, key := range []string{"working", "done", "idle", "unknown"} {
		if sudo <= glyphRank(StatusGlyph{Key: key}) {
			t.Fatal("sudo did not override", key)
		}
	}
}
