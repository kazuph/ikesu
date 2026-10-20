package tui

import (
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/teststate"
	"testing"
)

func TestTabStatusDecisionAttentionRestoresUnderlyingShellAndAgentState(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	for _, agent := range []string{"", "codex"} {
		a.SetReported(1, agent, "working")
		for _, pending := range []bool{true, false} {
			if pending {
				a.SetDecisionAttention(map[int]bool{1: true})
			} else {
				a.SetDecisionAttention(nil)
			}
			in := render.Input{State: a.st, Panes: a.paneSnaps()}
			out := render.Compose(in)
			r := out.TabHits[0]
			want := a.glyph(1)
			if out.Frame.Cell(r.X+1, r.Y).Content != want.Rune || out.Frame.Cell(r.X+1, r.Y).Style.Fg != out.Frame.ThemeColor(want.Color) {
				t.Fatal("tab attention differs from pane", agent, pending)
			}
			if pending && want.Key != "blocked" || !pending && want.Key != "working" {
				t.Fatal("attention did not restore underlying state", want)
			}
		}
	}
}
