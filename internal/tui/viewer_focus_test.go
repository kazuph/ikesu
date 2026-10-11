package tui

import (
	"bytes"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/runtime"
)

func TestViewerFocusAcknowledgmentCannotReleaseNewerKeys(t *testing.T) {
	app := NewApp(model.New(t.TempDir(), 80, 24), runtime.NewSupervisor(), &bytes.Buffer{}, "")
	viewer := app.NewViewer(&bytes.Buffer{}, 80, 24, nil)
	viewer.with(app, func(ui *App) { viewer.onInput(ui, input.Event{Kind: input.KindFocus}) })
	old := viewer.FocusEpoch()
	viewer.with(app, func(ui *App) {
		viewer.onInput(ui, input.Event{Kind: input.KindFocus})
		viewer.onInput(ui, input.Event{Kind: input.KindKey, Key: uv.Key{Code: 'x', Text: "x"}})
	})
	current := viewer.FocusEpoch()
	if current == old || len(viewer.heldKeys) != 1 {
		t.Fatal("new focus did not hold input")
	}
	viewer.Rendered(app, render.Output{}, old)
	if !viewer.AwaitingFocusRender() || len(viewer.heldKeys) != 1 {
		t.Fatal("stale frame released input")
	}
	viewer.Rendered(app, render.Output{}, current)
	if viewer.AwaitingFocusRender() || len(viewer.heldKeys) != 0 {
		t.Fatal("current frame failed to release input")
	}
	viewer.with(app, func(ui *App) {
		viewer.onInput(ui, input.Event{Kind: input.KindFocus})
		viewer.onInput(ui, input.Event{Kind: input.KindKey, Key: uv.Key{Code: 'y', Text: "y"}})
	})
	viewer.focusUntil = time.Now().Add(-time.Nanosecond)
	viewer.Tick(app)
	if viewer.AwaitingFocusRender() || len(viewer.heldKeys) != 0 {
		t.Fatal("deadline failed to release input")
	}
}
