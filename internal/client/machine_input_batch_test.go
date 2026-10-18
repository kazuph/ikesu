package client

import (
	"bytes"
	"os"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/machine"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/tui"
)

func TestMachineInputBatchPaintsOnceAndYieldsAtBound(t *testing.T) {
	for _, total := range []int{12, 600} {
		out, err := os.CreateTemp(t.TempDir(), "viewer")
		if err != nil {
			t.Fatal(err)
		}
		defer out.Close()
		state := model.New(t.TempDir(), 120, 45)
		for i := 0; i < 80; i++ {
			state.Apply(model.CmdNewWorkspace{Name: "scrollable"})
		}
		snapshot := &render.Input{State: state, Panes: map[int]*render.PaneSnap{}}
		m := &machineClient{active: "local", order: []string{"local", "remote"}, out: out, settings: &localSettings{}, size: tui.Winsize{Cols: 120, Rows: 45}, endpoints: map[string]*managedEndpoint{
			"local":  {profile: machine.Profile{ID: "local", Enabled: true}, snapshot: snapshot},
			"remote": {profile: machine.Profile{ID: "remote", Enabled: true}, snapshot: snapshot},
		}}
		if err := m.paint(); err != nil {
			t.Fatal(err)
		}
		if len(m.sidebar.Hits) == 0 {
			t.Fatal("no actual sidebar hit regions")
		}
		hit := m.sidebar.Hits[0]
		event := input.Event{Kind: input.KindMouseWheel, Mouse: uv.Mouse{X: hit.Rect.X, Y: hit.Rect.Y, Button: uv.MouseWheelDown}}
		queued := make(chan input.Event, total-1)
		for i := 1; i < total; i++ {
			queued <- event
		}
		before, _ := out.Seek(0, 2)
		count, err := m.drainInput(event, queued)
		if err != nil {
			t.Fatal(err)
		}
		want := min(total, 513)
		if count != want || len(queued) != total-want {
			t.Fatalf("processed=%d remaining=%d", count, len(queued))
		}
		data, err := os.ReadFile(out.Name())
		if err != nil {
			t.Fatal(err)
		}
		if frames := bytes.Count(data[before:], []byte("\x1b[?2026h")); frames != 1 {
			t.Fatalf("%d scroll inputs produced %d frames", count, frames)
		}
		if m.inputBatch {
			t.Fatal("paint suppression remained enabled")
		}
	}
}

func TestMachineInputBatchStopsForPendingDestination(t *testing.T) {
	m := &machineClient{pending: &pendingMachine{id: "remote", deadline: time.Now().Add(machineSwitchWait)}}
	event := input.Event{Kind: input.KindKey, Raw: []byte("a")}
	queued := make(chan input.Event, 1)
	queued <- input.Event{Kind: input.KindKey, Raw: []byte("b")}
	count, err := m.drainInput(event, queued)
	if err != nil || count != 1 || len(queued) != 1 || len(m.pending.keys) != 1 || string(m.pending.keys[0].Raw) != "a" {
		t.Fatalf("pending input drain: count=%d err=%v", count, err)
	}
}
