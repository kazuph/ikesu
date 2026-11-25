package client

import (
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/tui"
)

type machinePrune struct {
	dialog    *tui.PruneDialog
	active    string
	connected []string
	request   uint64
	pending   map[string][]tui.PruneEntry
	scanned   map[string]uint64
	executing bool
}

func (m *machineClient) pruneArea() layout.Rect { return layout.Rect{W: m.size.Cols, H: m.size.Rows} }

func (m *machineClient) openPruneWorkspaces() {
	if m.prune != nil {
		return
	}
	active := m.active
	label := active
	if e := m.endpoints[active]; e != nil {
		label = e.profile.Label
	}
	var labels, connected []string
	for _, id := range m.order {
		if e := m.endpoints[id]; e != nil && e.connection != nil && e.snapshotAttempt == e.attempt {
			labels = append(labels, e.profile.Label)
			connected = append(connected, id)
		}
	}
	m.prune = &machinePrune{dialog: tui.NewPruneDialog(label, labels), active: active, connected: connected}
}

func (m *machineClient) pruneInput(event input.Event) {
	switch m.prune.dialog.Input(m.mouse.CellEvent(event, m.size), m.pruneArea()) {
	case tui.PruneClose:
		m.closePruneWorkspaces()
	case tui.PruneStartScan:
		m.scanPruneWorkspaces()
	case tui.PruneStartExec:
		m.execPruneWorkspaces()
	}
}

func (m *machineClient) closePruneWorkspaces() {
	p := m.prune
	m.prune = nil
	if p.executing {
		return
	}
	for id := range p.pending {
		e := m.endpoints[id]
		if e == nil || e.connection == nil {
			continue
		}
		writer := e.connection.writer
		if endpoint.Allows(writer.available, endpoint.RequiredGeneration(endpoint.PruneScanCancel), writer.methods, endpoint.PruneScanCancel) {
			if err := e.connection.enqueue(msg{Type: endpoint.PruneScanCancel, PruneRequest: p.request}); err != nil {
				m.notify(e.profile.Label + ": " + err.Error())
			}
		}
	}
}

func (m *machineClient) scanPruneWorkspaces() {
	p := m.prune
	m.pruneRequest++
	p.request = m.pruneRequest
	p.pending = map[string][]tui.PruneEntry{}
	p.scanned = map[string]uint64{}
	machines := p.connected
	if p.dialog.LocalOnly() {
		machines = []string{p.active}
	}
	for _, id := range machines {
		e := m.endpoints[id]
		if e == nil {
			continue
		}
		if e.connection == nil || e.snapshotAttempt != e.attempt {
			p.dialog.ScanIssue(e.profile.Label, "disconnected")
			continue
		}
		if !supportsPrune(e) {
			p.dialog.ScanIssue(e.profile.Label, "unsupported")
			continue
		}
		if err := e.connection.enqueue(msg{Type: endpoint.PruneScan, PruneRequest: p.request}); err != nil {
			p.dialog.ScanIssue(e.profile.Label, err.Error())
			continue
		}
		p.pending[id] = nil
	}
	if len(p.pending) == 0 {
		p.dialog.Ready()
	}
}

func supportsPrune(e *managedEndpoint) bool {
	writer := e.connection.writer
	return endpoint.Allows(writer.available, endpoint.RequiredGeneration(endpoint.PruneScan), writer.methods, endpoint.PruneScan) &&
		endpoint.Allows(writer.available, endpoint.RequiredGeneration(endpoint.PruneExec), writer.methods, endpoint.PruneExec)
}

func (m *machineClient) pruneScanResult(id string, message msg) {
	p := m.prune
	if p == nil || p.request != message.PruneRequest || p.executing {
		return
	}
	if _, ok := p.pending[id]; !ok {
		return
	}
	delete(p.pending, id)
	e := m.endpoints[id]
	switch {
	case message.PruneError != "":
		p.dialog.ScanIssue(e.profile.Label, message.PruneError)
	case message.PruneScan == nil || message.PruneScan.ServerGeneration == "":
		p.dialog.ScanIssue(e.profile.Label, "invalid scan response")
	default:
		p.dialog.AddScan(id, e.profile.Label, *message.PruneScan)
		p.scanned[id] = e.attempt
	}
	if len(p.pending) == 0 {
		p.dialog.Ready()
	}
}

func (m *machineClient) execPruneWorkspaces() {
	p := m.prune
	m.pruneRequest++
	p.request = m.pruneRequest
	p.executing = true
	p.pending = map[string][]tui.PruneEntry{}
	groups := map[string][]tui.PruneEntry{}
	for _, entry := range p.dialog.Selection() {
		groups[entry.Machine] = append(groups[entry.Machine], entry)
	}
	for id, entries := range groups {
		e := m.endpoints[id]
		if attempt, ok := p.scanned[id]; !ok || e == nil || e.connection == nil || e.attempt != attempt || !supportsPrune(e) {
			addUnknownPruneResults(p, entries, endpoint.PruneSkipped, "connection changed since scan")
			continue
		}
		request := endpoint.PruneExecRequest{ServerGeneration: entries[0].Server}
		for _, entry := range entries {
			request.Candidates = append(request.Candidates, entry.Candidate)
		}
		if err := e.connection.enqueue(msg{Type: endpoint.PruneExec, PruneRequest: p.request, PruneExec: &request}); err != nil {
			addUnknownPruneResults(p, entries, endpoint.PruneFailed, err.Error())
			continue
		}
		p.pending[id] = entries
	}
	if len(p.pending) == 0 {
		p.dialog.Complete()
	}
}

func (m *machineClient) pruneExecResult(id string, message msg) {
	p := m.prune
	if p == nil || p.request != message.PruneRequest || !p.executing {
		return
	}
	entries, ok := p.pending[id]
	if !ok {
		return
	}
	delete(p.pending, id)
	switch {
	case message.PruneError != "":
		addUnknownPruneResults(p, entries, endpoint.PruneFailed, message.PruneError)
	case message.PruneResult == nil:
		addUnknownPruneResults(p, entries, "", "missing deletion result")
	default:
		results := map[int]endpoint.PruneOutcome{}
		for _, result := range message.PruneResult.Outcomes {
			results[result.Workspace] = result
		}
		for _, entry := range entries {
			result, ok := results[entry.Candidate.Workspace]
			if !ok {
				result.Reason = "missing deletion result"
			}
			p.dialog.AddOutcome(entry.Label, entry.Candidate.Name, result)
		}
	}
	if len(p.pending) == 0 {
		p.dialog.Complete()
	}
}

func addUnknownPruneResults(p *machinePrune, entries []tui.PruneEntry, state, reason string) {
	for _, entry := range entries {
		p.dialog.AddOutcome(entry.Label, entry.Candidate.Name, endpoint.PruneOutcome{State: state, Reason: reason})
	}
}

func (m *machineClient) pruneDisconnect(id string) {
	p := m.prune
	if p == nil {
		return
	}
	delete(p.scanned, id)
	entries, ok := p.pending[id]
	if !ok {
		return
	}
	delete(p.pending, id)
	if p.executing {
		addUnknownPruneResults(p, entries, "", "disconnected before deletion result")
		if len(p.pending) == 0 {
			p.dialog.Complete()
		}
		return
	}
	p.dialog.ScanIssue(m.endpoints[id].profile.Label, "disconnected during scan")
	if len(p.pending) == 0 {
		p.dialog.Ready()
	}
}
