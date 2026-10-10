package render

import (
	"github.com/kazuph/goterrace/internal/config"
	"image/color"
	"reflect"
	"testing"
)

func TestObservedDistributionThemeTokens(t *testing.T) {
	for _, sample := range []struct {
		name              string
		accent, red, teal color.RGBA
	}{
		{"kanagawa", color.RGBA{126, 156, 216, 255}, color.RGBA{195, 64, 67, 255}, color.RGBA{127, 180, 202, 255}},
		{"rose-pine", color.RGBA{196, 167, 231, 255}, color.RGBA{235, 111, 146, 255}, color.RGBA{156, 207, 216, 255}},
		{"vesper", color.RGBA{255, 199, 153, 255}, color.RGBA{255, 128, 128, 255}, color.RGBA{102, 221, 204, 255}},
	} {
		if err := config.ValidateTheme(sample.name); err != nil {
			t.Fatal(err)
		}
		p := ThemePalette(sample.name)
		if !reflect.DeepEqual(p.Accent, sample.accent) || !reflect.DeepEqual(p.Red, sample.red) || !reflect.DeepEqual(p.NavigatorDone, sample.teal) {
			t.Fatal(sample.name, p)
		}
	}
}
func TestConfigAccentCustomPrecedence(t *testing.T) {
	c := config.Config{}
	c.UI.Accent = "#123456"
	if !reflect.DeepEqual(ConfigPalette(c).Accent, color.RGBA{18, 52, 86, 255}) {
		t.Fatal(ConfigPalette(c))
	}
	c.Theme.Custom = map[string]string{"accent": "#abcdef"}
	if !reflect.DeepEqual(ConfigPalette(c).Accent, color.RGBA{171, 205, 239, 255}) {
		t.Fatal(ConfigPalette(c))
	}
}
