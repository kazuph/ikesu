package client

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/render"
	"strings"
	"time"
)

type machineSidebarAction struct {
	hit   render.MachineHit
	event input.Event
}

func (m *machineClient) rightMachineInput(hit render.MachineHit, event input.Event) error {
	if event.Kind != input.KindMouseClick || (event.Mouse.Button != uv.MouseLeft && event.Mouse.Button != uv.MouseRight) {
		return nil
	}
	if hit.Row.Kind != render.RowAgent && hit.Row.Kind != render.RowJob {
		return nil
	}
	e := m.endpoints[hit.Machine]
	if e.connection == nil || e.snapshot == nil {
		return fmt.Errorf("endpoint_action_unsupported: right sidebar target unavailable")
	}
	m.suppressRelease = true
	if m.active != hit.Machine {
		workspace := e.snapshot.State.ActiveWorkspace().ID
		if hit.Row.Kind == render.RowAgent {
			workspace = e.snapshot.State.Workspaces[hit.Row.Ws].ID
		}
		m.pending = &pendingMachine{id: hit.Machine, workspace: workspace, deadline: time.Now().Add(machineSwitchWait), sidebar: &machineSidebarAction{hit: hit, event: event}}
		m.requestPending()
		return nil
	}
	return m.sidebarInput(hit, event)
}

// Translate a composed machine row to the endpoint's own visible row using
// its existing wheel and button input. No pane-focus API or shared focus is used.
func (m *machineClient) rightSidebarBytes(hit render.MachineHit, event input.Event) ([]byte, error) {
	e := m.endpoints[hit.Machine]
	if e.connection == nil || e.snapshot == nil {
		return nil, fmt.Errorf("endpoint_action_unsupported: right sidebar target unavailable")
	}
	in := *e.snapshot
	area := in.State.RightSidebarArea()
	if area.W == 0 {
		return nil, fmt.Errorf("endpoint_action_unsupported: target right sidebar is hidden")
	}
	var button, rawX, rawY int
	var final byte
	if _, err := fmt.Sscanf(string(event.Raw), "\x1b[<%d;%d;%d%c", &button, &rawX, &rawY, &final); err != nil {
		return nil, fmt.Errorf("unsupported sidebar mouse encoding")
	}
	var commands strings.Builder
	encode := func(code, x, y int, suffix byte) error {
		ev := event
		ev.Mouse.X = x
		xx, yy, err := m.sidebarMousePosition(e, ev, y)
		if err != nil {
			return err
		}
		fmt.Fprintf(&commands, "\x1b[<%d;%d;%d%c", code, xx, yy, suffix)
		return nil
	}
	var rows []render.SidebarRow
	offset, height, y := in.AgentScroll, max(0, area.H-2), 2
	jobs := hit.Row.Kind == render.RowJob
	if jobs != in.JobsPanel {
		h := render.RightSidebarHeaderFor(in.State, 0)
		tab := h.Agents
		if jobs {
			tab = h.Jobs
		}
		if err := encode(0, tab.X, tab.Y, 'M'); err != nil {
			return nil, err
		}
		if err := encode(0, tab.X, tab.Y, 'm'); err != nil {
			return nil, err
		}
		if !jobs {
			offset = 0
		}
	}
	if jobs {
		offset = in.JobScroll
		for i := range in.Jobs {
			rows = append(rows, render.SidebarRow{Kind: render.RowJob, Line: i})
		}
	} else {
		rows = render.RightAgentRows(in)
	}
	index := -1
	for i, row := range rows {
		same := row == hit.Row
		if same {
			index = i
			break
		}
	}
	if index < 0 || height <= 0 {
		return nil, fmt.Errorf("endpoint_action_unsupported: right sidebar row unavailable")
	}
	offset = max(0, min(offset, max(0, len(rows)-height)))
	desired := offset
	if index < offset {
		desired = index
	} else if index >= offset+height {
		desired = index - height + 1
	}
	wheel := 65
	if desired < offset {
		wheel = 64
	}
	for i := 0; i < max(desired-offset, offset-desired); i++ {
		if err := encode(wheel, area.X+1, y, 'M'); err != nil {
			return nil, err
		}
	}
	if err := encode(button, area.X+1, y+index-desired, 'M'); err != nil {
		return nil, err
	}
	if err := encode(button, area.X+1, y+index-desired, 'm'); err != nil {
		return nil, err
	}
	return []byte(commands.String()), nil
}
