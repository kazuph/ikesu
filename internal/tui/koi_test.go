package tui

import (
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/teststate"
	"testing"
)

func TestKoiStandaloneClockCannotTriggerSnapshotSink(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.SetReported(a.focusedPane(), "codex", "idle")
	a.draw()
	if !a.koiLocalActive {
		t.Fatal("standalone fish clock not active")
	}
	a.snapshotSink = func(in render.Input) error {
		if in.Koi || in.KoiTime != 0 || in.KoiGlow != nil {
			t.Fatal("decoration leaked into snapshot")
		}
		return nil
	}
	a.draw()

	off := false
	a.sidebarConfig.UI.Koi = &off
	a.snapshotSink = nil
	a.draw()
	if a.koiLocalActive {
		t.Fatal("off kept standalone animation active")
	}
}
