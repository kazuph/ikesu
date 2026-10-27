package client

import (
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
)

type workspaceCycleTarget struct {
	machine   string
	workspace int
}

func workspaceCycleCommand(c config.Config, key uv.Key) bool {
	for _, binding := range c.Keys.Command {
		chord, err := config.ParseChord(binding.Key)
		if err == nil && chord.Match(key, false) {
			return true
		}
	}
	return false
}

func workspaceCycleDirection(c config.Config, key uv.Key) int {
	if workspaceCycleCommand(c, key) {
		return 0
	}
	for _, action := range []struct {
		name string
		dir  int
	}{{"next_workspace", 1}, {"previous_workspace", -1}} {
		for _, binding := range c.ActionBindings(action.name) {
			chord, err := config.ParseChord(binding)
			if err == nil && chord.Match(key, false) {
				return action.dir
			}
		}
	}
	return 0
}

func (m *machineClient) workspaceCycleTargets() []workspaceCycleTarget {
	var targets []workspaceCycleTarget
	for _, id := range m.order {
		e := m.endpoints[id]
		if e == nil || !e.profile.Enabled || e.connection == nil || e.snapshot == nil || e.snapshot.State == nil {
			continue
		}
		for _, index := range e.snapshot.State.VisibleWorkspaceOrder() {
			targets = append(targets, workspaceCycleTarget{id, e.snapshot.State.Workspaces[index].ID})
		}
	}
	return targets
}

func (m *machineClient) workspaceCycleInput(event input.Event) bool {
	if event.Kind == input.KindBlur {
		m.workspaceKeyReleases = nil
		return false
	}
	if event.Kind != input.KindKey && event.Kind != input.KindKeyRelease {
		return false
	}
	key := event.Key.Code
	if event.Kind == input.KindKeyRelease {
		if m.workspaceKeyReleases[key] {
			delete(m.workspaceKeyReleases, key)
			return true
		}
		return false
	}
	local, active := m.endpoints["local"], m.endpoints[m.active]
	if len(m.order) < 2 || local == nil || local.snapshot == nil || active == nil || active.connection == nil || active.snapshot == nil || (m.regions == nil && m.pending == nil) {
		return false
	}
	if active.snapshotAttempt != active.attempt || local.snapshotAttempt != local.attempt {
		return false
	}
	view := *active.snapshot
	if m.forms != nil {
		m.forms.Overlay(&view)
	}
	if view.State == nil || view.Dialog != nil || view.Menu != nil || view.Help != nil || view.Navigator != nil || view.Settings != nil || view.Prefix || view.Copy != nil || view.ModeHint != "" {
		return false
	}
	dir := workspaceCycleDirection(local.snapshot.SidebarConfig, event.Key)
	if dir == 0 || workspaceCycleCommand(active.snapshot.SidebarConfig, event.Key) {
		return false
	}
	if !endpoint.Allows(active.connection.writer.available, endpoint.InputReceiptGeneration, active.connection.writer.methods, "input_receipt") {
		return false
	}
	sent := active.connection.lastSentInput()
	if sent > active.processedInput && !(m.pending != nil && m.pending.id == m.active && sent == m.pending.inputSequence) {
		return false
	}
	prefix := active.snapshot.SidebarConfig.Keys.Prefix
	if prefix == "" {
		prefix = "ctrl+b"
	}
	if chord, err := config.ParseChord(prefix); err == nil && chord.Match(event.Key, false) {
		return false
	}
	targets := m.workspaceCycleTargets()
	if len(targets) == 0 {
		return false
	}
	at := -1
	ws := view.State.ActiveWorkspace()
	current := workspaceCycleTarget{machine: m.active}
	if ws != nil {
		current.workspace = ws.ID
	}
	if m.pending != nil {
		current = workspaceCycleTarget{m.pending.id, m.pending.workspace}
	}
	for i, target := range targets {
		if target == current {
			at = i
			break
		}
	}
	if at < 0 && dir < 0 {
		at = 0
	}
	target := targets[(at+dir+len(targets))%len(targets)]
	m.consumeWorkspaceKey(key)
	pending := &pendingMachine{id: target.machine, workspace: target.workspace, deadline: time.Now().Add(machineSwitchWait)}
	if m.pending != nil {
		pending.deadline = m.pending.deadline
		pending.keys = m.pending.keys
	}
	m.pending = pending
	m.requestPending()
	return true
}

func (m *machineClient) consumeWorkspaceKey(key rune) {
	if m.workspaceKeyReleases == nil {
		m.workspaceKeyReleases = map[rune]bool{}
	}
	m.workspaceKeyReleases[key] = true
}
