package client

import (
	"io"

	"github.com/kazuph/goterrace/internal/render"
)

type titleView struct{ workspace, tab int }

type windowTitleState struct {
	seen      map[titleView]bool
	view      titleView
	remaining int
}

// The isolated 0.2.24 byte captures show two title writes after paints
// on attach and returning to an existing tab in the same workspace.
// Workspace changes, new tabs and pane OSC titles do not resend it.
const windowTitleActivationPaints = 2

func (title *windowTitleState) painted(out io.Writer, frame render.Input) error {
	ws := frame.State.ActiveWorkspace()
	if ws == nil || ws.TabRef() == nil {
		return nil
	}
	view := titleView{ws.ID, ws.TabRef().ID}
	if title.seen == nil {
		title.seen = map[titleView]bool{view: true}
		title.remaining = windowTitleActivationPaints
	} else if view != title.view {
		if title.seen[view] && view.workspace == title.view.workspace {
			title.remaining = windowTitleActivationPaints
		} else {
			title.remaining = 0
		}
		title.seen[view] = true
	}
	title.view = view
	if title.remaining == 0 {
		return nil
	}
	if _, err := io.WriteString(out, "\x1b]0;gtx\x07"); err != nil {
		return err
	}
	title.remaining--
	return nil
}
