package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/teststate"
	"testing"
)

func TestStage5PartialCopyButtonRoutesEveryVisibleCell(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	for width := 1; width <= 5; width++ {
		a.exec(model.CmdResize{Cols: 26 + width, Rows: 30})
		a.actionBar = render.Compose(render.Input{State: a.st}).ActionBar
		for x := 26; x < 26+width; x++ {
			if !a.actionBarHit(uv.Mouse{X: x, Y: 29, Button: uv.MouseLeft}) || a.cm == nil {
				t.Fatalf("width=%d click=%d did not enter copy", width, x)
			}
			a.exitCopy()
		}
	}
}
