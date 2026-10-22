package render

import (
	"testing"

	"github.com/kazuph/ikesu/internal/model"
)

func TestConfigBannerPreservesSidebarTabsAndAddButton(t *testing.T) {
	for _, cols := range []int{60, 100, 140} {
		s := model.New(t.TempDir(), cols, 30)
		base := Compose(Input{State: s})
		out := Compose(Input{State: s, ConfigWarningBanner: true, ConfigWarning: "config.toml invalid; using defaults; ikesu config check"})
		left := base.NewTabRect.X + base.NewTabRect.W
		if out.ConfigWarningRect.X <= left || out.ConfigWarningRect.Y != 0 {
			t.Fatal(cols, out.ConfigWarningRect, base.NewTabRect)
		}
		for x := 0; x < left; x++ {
			if *base.Frame.Cell(x, 0) != *out.Frame.Cell(x, 0) {
				t.Fatal("warning overwrote tab/sidebar", cols, x)
			}
		}
	}
}
