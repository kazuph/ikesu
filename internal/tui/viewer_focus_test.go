package tui

import (
	"bytes"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/runtime"
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

func TestViewerInputReceiptWaitsForFramingAndFocusDelivery(t *testing.T) {
	app := NewApp(model.New(t.TempDir(), 80, 24), runtime.NewSupervisor(), &bytes.Buffer{}, "")
	viewer := app.NewViewer(&bytes.Buffer{}, 80, 24, nil)
	viewer.Input(app, []byte("\x1b["))
	viewer.RecordInput(app, 1)
	if viewer.ProcessedInput() != 0 {
		t.Fatal("incomplete sequence acknowledged")
	}
	viewer.Input(app, []byte("9u"))
	viewer.RecordInput(app, 2)
	if viewer.ProcessedInput() != 2 {
		t.Fatal("complete input not acknowledged")
	}
	viewer.Input(app, []byte("\x1b[I\x02"))
	viewer.RecordInput(app, 3)
	if viewer.ProcessedInput() != 2 {
		t.Fatal("focus-held prefix acknowledged before delivery")
	}
	viewer.Rendered(app, render.Output{}, viewer.FocusEpoch())
	if viewer.ProcessedInput() != 3 || !viewer.app.prefix {
		t.Fatal("receipt did not follow actual prefix delivery")
	}
}
