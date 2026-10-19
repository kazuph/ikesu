package render

import "io"

// WindowTitle uses the sidebar's workspace aggregation, including off-screen
// workspaces and the other connected machines.
func WindowTitle(frame Input, session string, groups []MachineGroup) string {
	name := "gtx"
	if session != "" && session != "default" {
		name += ":" + session
	}
	if frame.SidebarConfig.UI.WindowTitle == "plain" {
		return name
	}
	states := map[string]bool{}
	collect := func(in *Input) {
		if in == nil || in.State == nil {
			return
		}
		for _, ws := range in.State.Workspaces {
			states[aggregateGlyph(ws, *in).Key] = true
		}
	}
	if len(groups) == 0 {
		collect(&frame)
	} else {
		for _, group := range groups {
			collect(group.Input)
		}
	}
	marks := ""
	for _, state := range []struct{ key, mark string }{{"blocked", "🔴"}, {"working", "🟡"}, {"done", "🔵"}} {
		if states[state.key] {
			marks += state.mark
		}
	}
	if marks != "" {
		return marks + " " + name
	}
	return name
}

type titleView struct{ workspace, tab int }

type WindowTitleState struct {
	seen      map[titleView]bool
	view      titleView
	remaining int
	last      string
}

// Preserve the two title writes on attach and returning to an existing tab.
const windowTitleActivationPaints = 2

func (title *WindowTitleState) Painted(out io.Writer, frame Input, session string, groups []MachineGroup) error {
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
	value := WindowTitle(frame, session, groups)
	if title.remaining == 0 && value == title.last {
		return nil
	}
	if _, err := io.WriteString(out, "\x1b]0;"+value+"\x07"); err != nil {
		return err
	}
	title.last = value
	if title.remaining > 0 {
		title.remaining--
	}
	return nil
}
