package tui

import (
	"io"
	"testing"

	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/teststate"
)

// An attached screen must follow the full density's preview line after it
// connected, like the git line and the space name.
func TestViewerSnapshotFollowsWorkspacePreview(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	v := a.NewViewer(io.Discard, 120, 35, nil)
	var got render.Input
	v.SetSnapshotSink(func(in render.Input) error { got = in; return nil })
	id := a.st.Workspaces[0].ID
	for _, text := range []string{"first line", "second line"} {
		a.workspacePreviews = map[int]string{id: text}
		v.Draw(a, nil)
		if got.WorkspacePreviews[id] != text {
			t.Fatalf("viewer snapshot preview=%q want %q", got.WorkspacePreviews[id], text)
		}
	}
}
