package render

import (
	"github.com/kazuph/goterrace/internal/model"
	"testing"
)

func TestNavigatorWorkingFilterUsesEndpointFrameAndStandaloneInitialGlyph(t *testing.T) {
	state := model.New(t.TempDir(), 160, 45)
	for _, server := range []bool{false, true} {
		for spin, glyph := range []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"} {
			in := Input{State: state, ServerUI: server, Spin: spin, Navigator: &Navigator{Filter: "working"}}
			f := Compose(in).Frame
			if !server {
				glyph = "⠋"
			}
			if got := f.Cell(14, 5).Content; got != glyph {
				t.Fatalf("endpoint=%t spin=%d: working filter %q want %q", server, spin, got, glyph)
			}
		}
	}
}
