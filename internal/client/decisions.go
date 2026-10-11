package client

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/render"
	"sort"
	"time"
)

type queuedDecision struct {
	machine string
	record  api.Decision
}
type decisionUI struct {
	queue                      []queuedDecision
	key                        string
	closed, focused, submitted bool
	selected, scroll           int
	text                       string
	regions                    render.DecisionRegions
	countdown                  time.Time
}

func (m *machineClient) updateDecisionQueue() {
	var queue []queuedDecision
	for id, e := range m.endpoints {
		if e.connection == nil || !e.profile.Enabled || e.decisions == nil {
			continue
		}
		for _, d := range e.decisions.Pending {
			if d.Status == "pending" {
				queue = append(queue, queuedDecision{id, d})
			}
		}
	}
	sort.Slice(queue, func(i, j int) bool {
		a, b := queue[i], queue[j]
		if a.record.CreatedUnixMS != b.record.CreatedUnixMS {
			return a.record.CreatedUnixMS < b.record.CreatedUnixMS
		}
		if a.machine != b.machine {
			return a.machine < b.machine
		}
		return a.record.DecisionID < b.record.DecisionID
	})
	old := m.decisions.key
	m.decisions.queue = queue
	index := -1
	for i, d := range queue {
		if d.machine+"/"+d.record.DecisionID == old {
			index = i
			break
		}
	}
	if index != 0 {
		m.decisions.key = ""
		m.decisions.closed = false
		m.decisions.focused = false
		m.decisions.selected = 0
		m.decisions.scroll = 0
		m.decisions.text = ""
		m.decisions.submitted = false
		if len(queue) > 0 {
			m.decisions.key = queue[0].machine + "/" + queue[0].record.DecisionID
		}
	}
	if len(queue) == 0 {
		m.decisions.regions = render.DecisionRegions{}
	}
	m.decisions.countdown = time.Time{}
	if d := m.currentDecision(); d != nil && !m.decisions.closed && d.record.ExpiresUnixMS != nil {
		m.decisions.countdown = time.Now().Add(time.Second)
	}
}
func (m *machineClient) currentDecision() *queuedDecision {
	for i := range m.decisions.queue {
		d := &m.decisions.queue[i]
		if d.machine+"/"+d.record.DecisionID == m.decisions.key {
			return d
		}
	}
	return nil
}
func (m *machineClient) acceptDecisions(id string, p *endpoint.DecisionProjection) {
	e := m.endpoints[id]
	if p == nil {
		return
	}
	if old := e.decisions; old != nil && old.BootID == p.BootID && old.Revision >= p.Revision {
		return
	}
	e.decisions = p
	m.updateDecisionQueue()
}
func (m *machineClient) paintDecisions(out *render.Output) {
	var dialog *render.DecisionDialog
	if d := m.currentDecision(); d != nil && !m.decisions.closed {
		index := 0
		for i, q := range m.decisions.queue {
			if q.machine == d.machine && q.record.DecisionID == d.record.DecisionID {
				index = i
				break
			}
		}
		dialog = &render.DecisionDialog{Decision: d.record, Machine: m.endpoints[d.machine].profile.Label, Index: index, Total: len(m.decisions.queue), Selected: m.decisions.selected, Text: m.decisions.text, Focused: m.decisions.focused, Scroll: m.decisions.scroll}
	}
	m.decisions.regions = render.ComposeDecision(out, dialog, len(m.decisions.queue), time.Now())
}
func inDecisionRect(r layout.Rect, x, y int) bool {
	return r.W > 0 && r.H > 0 && contains(r.X, r.Y, r.W, r.H, x, y)
}
func (m *machineClient) decisionInput(ev input.Event) (bool, error) {
	d := m.currentDecision()
	if d == nil {
		return false, nil
	}
	ui := &m.decisions
	r := ui.regions
	if ev.Kind == input.KindKey && ev.Key.Code == uv.KeyEscape {
		if ui.closed {
			return false, nil
		}
		ui.closed = true
		ui.focused = false
		ui.countdown = time.Time{}
		return true, nil
	}
	if ev.Kind == input.KindMouseClick && ev.Mouse.Button == uv.MouseLeft {
		x, y := ev.Mouse.X, ev.Mouse.Y
		if inDecisionRect(r.Marker, x, y) {
			ui.closed = false
			ui.focused = false
			if d.record.ExpiresUnixMS != nil {
				ui.countdown = time.Now().Add(time.Second)
			}
			return true, nil
		}
		if ui.closed {
			return false, nil
		}
		if inDecisionRect(r.Close, x, y) {
			ui.closed = true
			ui.focused = false
			ui.countdown = time.Time{}
			return true, nil
		}
		if !inDecisionRect(r.Outer, x, y) {
			ui.focused = false
			return false, nil
		}
		ui.focused = true
		for i, rect := range r.Options {
			if inDecisionRect(rect, x, y) {
				ui.selected = i
				return true, m.answerDecision(d)
			}
		}
		return true, nil
	}
	if !ui.closed && ev.Kind == input.KindMouseWheel && inDecisionRect(r.Outer, ev.Mouse.X, ev.Mouse.Y) {
		if ev.Mouse.Button == uv.MouseWheelUp {
			ui.scroll = max(0, ui.scroll-1)
		} else {
			ui.scroll = min(max(0, r.ScrollRows-r.VisibleRows), ui.scroll+1)
		}
		return true, nil
	}
	if ui.closed || !ui.focused {
		return false, nil
	}
	if ev.Kind == input.KindPaste {
		if d.record.AllowText {
			ui.text += ev.Text
		}
		return true, nil
	}
	if ev.Kind != input.KindKey {
		return false, nil
	}
	switch ev.Key.Code {
	case uv.KeyUp:
		ui.selected = max(0, ui.selected-1)
	case uv.KeyDown:
		ui.selected = min(len(d.record.Options)-1, ui.selected+1)
	case uv.KeyEnter:
		return true, m.answerDecision(d)
	case uv.KeyPgUp:
		ui.scroll = max(0, ui.scroll-r.VisibleRows)
	case uv.KeyPgDown:
		ui.scroll = min(max(0, r.ScrollRows-r.VisibleRows), ui.scroll+r.VisibleRows)
	case uv.KeyBackspace:
		if runes := []rune(ui.text); len(runes) > 0 {
			ui.text = string(runes[:len(runes)-1])
		}
	default:
		if d.record.AllowText && ev.Key.Mod&(uv.ModCtrl|uv.ModAlt|uv.ModSuper) == 0 {
			ui.text += ev.Key.Text
		}
	}
	return true, nil
}
func (m *machineClient) answerDecision(d *queuedDecision) error {
	if m.decisions.submitted {
		return nil
	}
	e := m.endpoints[d.machine]
	if e.connection == nil {
		return fmt.Errorf("decision owner disconnected")
	}
	option := d.record.Options[m.decisions.selected].ID
	p := api.DecisionAnswerParams{DecisionID: d.record.DecisionID, OptionID: &option, Responder: "client"}
	if d.record.AllowText && m.decisions.text != "" {
		text := m.decisions.text
		p.Text = &text
	}
	if err := e.connection.enqueue(msg{Type: "decision_answer", DecisionAnswer: &p}); err != nil {
		return err
	}
	m.decisions.submitted = true
	return nil
}
