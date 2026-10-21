package endpoint

import (
	"reflect"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
)

func TestSnapshotJobRowsSurvivePaneResizeDeltasAndSuccessfulExit(t *testing.T) {
	zero := int64(0)
	in := render.Input{State: model.New("/tmp", 160, 45), ServerUI: true, JobsPanel: true,
		Panes: map[int]*render.PaneSnap{1: {Grid: [][]uv.Cell{{{Content: "first", Width: 1}}}}},
		Jobs:  []render.JobRow{{ID: "j0001", Label: "ROW", Status: "exited", CallerPane: "p1", Color: ansi.IndexedColor(2), ExitCode: &zero, AgeSinceMs: &zero}},
	}
	var encoder SnapshotEncoder
	var decoder SnapshotDecoder
	for _, cols := range []int{160, 80, 160} {
		in.State.Cols = cols
		in.Panes[1].Grid[0][0].Content = "changed"
		data, err := encoder.Encode(in)
		if err != nil {
			t.Fatal(err)
		}
		got, err := decoder.Decode(data)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.Jobs, in.Jobs) {
			t.Fatal("job row or successful zero exit changed across snapshot", got.Jobs, in.Jobs)
		}
		want, actual := render.Compose(in), render.Compose(got)
		if !reflect.DeepEqual(want.Frame, actual.Frame) {
			t.Fatal("visible job panel changed across pane/layout delta")
		}
	}
	in.Jobs = nil
	data, err := encoder.Encode(in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decoder.Decode(data)
	if err != nil || len(got.Jobs) != 0 {
		t.Fatal("removed job remained in the next snapshot", got.Jobs, err)
	}
}
