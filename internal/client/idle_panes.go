package client

import (
	"fmt"
	"sort"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/render"
)

type idleEntry struct {
	machine, label, server string
	pane                   endpoint.IdlePane
	selected               bool
}
type idleDialog struct {
	request                       uint64
	entries                       []idleEntry
	pending                       map[string]int
	threshold, cursor             int
	loading, confirming, deleting bool
	deleted, failed               int
	kept                          bool
}

var idleThresholds = []time.Duration{24 * time.Hour, 3 * 24 * time.Hour, 7 * 24 * time.Hour}
var idleThresholdLabels = []string{"24h", "3d", "1w"}

func (m *machineClient) openIdlePanes() {
	m.idleRequest++
	d := &idleDialog{request: m.idleRequest, pending: map[string]int{}, loading: true}
	m.idle = d
	for _, id := range m.order {
		e := m.endpoints[id]
		if e == nil || e.connection == nil || e.snapshotAttempt != e.attempt {
			continue
		}
		if !endpoint.Allows(e.connection.writer.available, endpoint.RequiredGeneration("idle_panes"), e.connection.writer.methods, "idle_panes") {
			continue
		}
		if e.connection.enqueue(msg{Type: "idle_panes", IdleRequest: d.request}) == nil {
			d.pending[id] = 1
		}
	}
	if len(d.pending) == 0 {
		d.loading = false
	}
}

func (m *machineClient) idleListResult(id string, request uint64, list *endpoint.IdlePanes) {
	d := m.idle
	if d == nil || d.request != request || !d.loading || d.pending[id] == 0 {
		return
	}
	delete(d.pending, id)
	if list != nil {
		d.kept = d.kept || list.LastPaneKept
		e := m.endpoints[id]
		for _, p := range list.Panes {
			hq := m.oyagoi != nil && m.oyagoi.Machine == id && m.oyagoi.Session == m.selectionSession(id) && m.oyagoi.Workspace == p.Workspace && m.oyagoi.Generation == p.WorkspaceGeneration
			checked := idleInitiallySelected(p, hq, id == m.active)
			d.entries = append(d.entries, idleEntry{machine: id, label: e.profile.Label, server: list.ServerGeneration, pane: p, selected: checked})
		}
	}
	if len(d.pending) == 0 {
		d.loading = false
		sort.SliceStable(d.entries, func(i, j int) bool { return d.entries[i].pane.AgeNanos > d.entries[j].pane.AgeNanos })
	}
}

func idleInitiallySelected(p endpoint.IdlePane, hq, activeMachine bool) bool {
	return (p.State == "idle" || p.State == "done") && p.InputKnown && !p.PendingInput && !(p.Focused && activeMachine) && !hq
}
func (d *idleDialog) visible() []int {
	var out []int
	for i := range d.entries {
		if time.Duration(d.entries[i].pane.AgeNanos) > idleThresholds[d.threshold] {
			out = append(out, i)
		}
	}
	return out
}
func (d *idleDialog) selected() int {
	n := 0
	for _, i := range d.visible() {
		if d.entries[i].selected {
			n++
		}
	}
	return n
}
func (d *idleDialog) dialog() *render.Dialog {
	if d.confirming {
		return &render.Dialog{Title: "delete idle panes", Detail: fmt.Sprintf("delete %d panes? This stops their processes.", d.selected()), Confirm: "delete", Cancel: "cancel", CenterTerminal: true, PreserveTitleCase: true}
	}
	labels := [3]string{}
	for i, label := range idleThresholdLabels {
		if i == d.threshold {
			labels[i] = "● " + label
		} else {
			labels[i] = "○ " + label
		}
	}
	detail := fmt.Sprintf("idle for: [%s] [%s] [%s] (tab)", labels[0], labels[1], labels[2])
	if d.loading {
		detail = "loading panes…"
	}
	if d.deleting {
		detail = "deleting panes…"
	}
	lines := []string{}
	visible := d.visible()
	if d.cursor >= len(visible) {
		d.cursor = max(0, len(visible)-1)
	}
	start := max(0, d.cursor-6)
	end := min(len(visible), start+7)
	for _, i := range visible[start:end] {
		e := d.entries[i]
		mark := " "
		if e.selected {
			mark = "x"
		}
		pointer := " "
		if len(visible) > 0 && visible[d.cursor] == i {
			pointer = ">"
		}
		age := time.Duration(e.pane.AgeNanos).Truncate(time.Hour)
		state := e.pane.State
		if !e.pane.InputKnown {
			state += " · input unknown"
		}
		lines = append(lines, fmt.Sprintf("%s [%s] p%d · %s · %s / %s · %s · idle %s · %s", pointer, mark, e.pane.ID, e.label, e.pane.WorkspaceName, e.pane.TabName, e.pane.Agent, age, state))
	}
	if d.kept {
		lines = append(lines, "last pane, kept")
	}
	lines = append(lines, "space toggle · a all · enter delete selected")
	return &render.Dialog{Title: "delete idle panes", Detail: detail, Lines: lines, Confirm: "delete selected", Cancel: "close", CenterTerminal: true, PreserveTitleCase: true}
}
func (m *machineClient) idleInput(ev input.Event) bool {
	d := m.idle
	if ev.Kind == input.KindMouseRelease && ev.Mouse.Button == uv.MouseLeft && !d.loading && !d.deleting {
		mouse := m.mouse.CellEvent(ev, m.size).Mouse
		_, cancel, confirm := render.DialogRects(d.dialog(), layout.Rect{W: m.size.Cols, H: m.size.Rows})
		if contains(cancel.X, cancel.Y, cancel.W, cancel.H, mouse.X, mouse.Y) {
			if d.confirming {
				d.confirming = false
			} else {
				m.idle = nil
			}
		} else if contains(confirm.X, confirm.Y, confirm.W, confirm.H, mouse.X, mouse.Y) {
			if d.confirming {
				m.deleteIdlePanes()
			} else if d.selected() > 0 {
				d.confirming = true
			}
		}
		return true
	}
	if ev.Kind != input.KindKey {
		return true
	}
	if ev.Key.Code == uv.KeyEscape {
		if d.confirming {
			d.confirming = false
		} else if !d.deleting {
			m.idle = nil
		}
		return true
	}
	if d.loading || d.deleting {
		return true
	}
	if d.confirming {
		if ev.Key.Code == uv.KeyEnter {
			m.deleteIdlePanes()
		}
		return true
	}
	visible := d.visible()
	switch ev.Key.Code {
	case uv.KeyTab:
		d.threshold = (d.threshold + 1) % len(idleThresholds)
		d.cursor = 0
	case uv.KeyUp:
		if d.cursor > 0 {
			d.cursor--
		}
	case uv.KeyDown:
		if d.cursor+1 < len(visible) {
			d.cursor++
		}
	case ' ':
		if len(visible) > 0 {
			e := &d.entries[visible[d.cursor]]
			e.selected = !e.selected
		}
	case 'a':
		for _, i := range visible {
			d.entries[i].selected = true
		}
	case uv.KeyEnter:
		if d.selected() > 0 {
			d.confirming = true
		}
	}
	return true
}
func (m *machineClient) deleteIdlePanes() {
	d := m.idle
	grouped := map[string]*endpoint.IdleDelete{}
	for _, i := range d.visible() {
		e := d.entries[i]
		if !e.selected {
			continue
		}
		if grouped[e.machine] == nil {
			grouped[e.machine] = &endpoint.IdleDelete{ServerGeneration: e.server}
		}
		grouped[e.machine].Panes = append(grouped[e.machine].Panes, e.pane)
	}
	d.confirming = false
	d.deleting = true
	d.pending = map[string]int{}
	for id, request := range grouped {
		e := m.endpoints[id]
		n := len(request.Panes)
		if e == nil || e.connection == nil || !endpoint.Allows(e.connection.writer.available, endpoint.RequiredGeneration("idle_delete"), e.connection.writer.methods, "idle_delete") || e.connection.enqueue(msg{Type: "idle_delete", IdleRequest: d.request, IdleDelete: request}) != nil {
			d.failed += n
			continue
		}
		d.pending[id] = n
	}
	m.finishIdleDelete()
}
func (m *machineClient) idleDeleteResult(id string, request uint64, result *endpoint.IdleDeleteResult) {
	d := m.idle
	if d == nil || d.request != request || !d.deleting || d.pending[id] == 0 {
		return
	}
	n := d.pending[id]
	delete(d.pending, id)
	if result == nil || result.Deleted < 0 || result.Failed < 0 || result.Deleted+result.Failed != n {
		d.failed += n
	} else {
		d.deleted += result.Deleted
		d.failed += result.Failed
	}
	m.finishIdleDelete()
}
func (m *machineClient) idleDisconnect(id string) {
	d := m.idle
	if d == nil || d.pending[id] == 0 {
		return
	}
	n := d.pending[id]
	delete(d.pending, id)
	if d.deleting {
		d.failed += n
		m.finishIdleDelete()
	} else if len(d.pending) == 0 {
		d.loading = false
		sort.SliceStable(d.entries, func(i, j int) bool { return d.entries[i].pane.AgeNanos > d.entries[j].pane.AgeNanos })
	}
}
func (m *machineClient) finishIdleDelete() {
	d := m.idle
	if d == nil || !d.deleting || len(d.pending) > 0 {
		return
	}
	m.idleToast = fmt.Sprintf("deleted %d · failed %d", d.deleted, d.failed)
	m.noticeUntil = time.Now().Add(connectionNotice)
	m.idle = nil
}
