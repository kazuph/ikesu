package client

import (
	"fmt"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/render"
)

type runawaySelection struct{ machine, id string }

type runawayFocusRequest struct {
	selection       runawaySelection
	pane, workspace int
}

var runawayActions = [...]string{endpoint.RunawayTerm, endpoint.RunawayKill, endpoint.RunawayFocus, endpoint.RunawaySnooze}

func (m *machineClient) runawayEndpoint(machine string) *managedEndpoint {
	e := m.endpoints[machine]
	if e == nil || e.connection == nil || e.connection.writer == nil ||
		!endpoint.Allows(e.connection.writer.available, 1, e.connection.writer.methods, endpoint.Runaway) {
		return nil
	}
	return e
}

func (m *machineClient) findRunaway(selection runawaySelection) (endpoint.RunawayAlert, bool) {
	if e := m.runawayEndpoint(selection.machine); e != nil {
		for _, a := range e.runawayAlerts {
			if a.ID == selection.id {
				return a, true
			}
		}
	}
	return endpoint.RunawayAlert{}, false
}

func (m *machineClient) overlayRunaway(in *render.Input) (toast, dialog runawaySelection) {
	if in.Dialog != nil || in.Menu != nil || in.Help != nil || in.Settings != nil ||
		in.Navigator != nil || in.Approval != nil || in.MobileSwitcher || m.search != nil {
		return
	}
	if a, ok := m.findRunaway(m.runawayOpen); ok {
		in.Toast = nil
		if a.Machine == "" {
			a.Machine = m.runawayOpen.machine
		}
		in.Dialog = runawayDialog(a)
		if m.runawayFocusError != "" {
			if in.Dialog.Detail != "" {
				in.Dialog.Detail += "\n"
			}
			in.Dialog.Detail += m.runawayFocusError
		}
		in.Dialog.MaxCols, in.Dialog.MaxRows = m.size.Cols, m.size.Rows
		if len(m.runawaySelections()) > 1 {
			in.Dialog.Actions = append(in.Dialog.Actions, "5 次の警告")
		}
		return toast, m.runawayOpen
	}
	m.runawayOpen = runawaySelection{}
	if in.Toast != nil || in.Copied != "" {
		return
	}
	addToast := func(machine string) bool {
		e := m.runawayEndpoint(machine)
		if e == nil || len(e.runawayAlerts) == 0 {
			return false
		}
		a := e.runawayAlerts[0]
		toast = runawaySelection{machine, a.ID}
		in.Toast = &render.Toast{
			Title: fmt.Sprintf("⚠ 暴走検知 s%d/p%d (%s)", a.Workspace, a.Pane, a.Name),
			Body:  fmt.Sprintf("%s RSS · クリックで対処", runawayMemory(max(a.RSS, a.TreeRSS))),
			Kind:  "runaway",
		}
		return true
	}
	if !addToast(m.active) {
		for _, machine := range m.order {
			if machine != m.active && addToast(machine) {
				break
			}
		}
	}
	return
}

func (m *machineClient) runawaySelections() []runawaySelection {
	var all []runawaySelection
	for _, machine := range m.order {
		if e := m.runawayEndpoint(machine); e != nil {
			for _, a := range e.runawayAlerts {
				all = append(all, runawaySelection{machine, a.ID})
			}
		}
	}
	return all
}

func (m *machineClient) runawayChoose(index int) {
	if index < len(runawayActions) {
		m.sendRunawayAction(m.runawayPainted, runawayActions[index])
		return
	}
	all := m.runawaySelections()
	for i, choice := range all {
		if choice == m.runawayPainted {
			m.inspectRunaway(all[(i+1)%len(all)])
			return
		}
	}
}

func runawayMemory(kib int64) string {
	if kib >= 1024*1024 {
		return fmt.Sprintf("%.1f GiB", float64(kib)/(1024*1024))
	}
	return fmt.Sprintf("%.1f MiB", float64(kib)/1024)
}

func runawayDialog(a endpoint.RunawayAlert) *render.Dialog {
	actions := []string{
		"1 安全停止 (SIGTERM)", "2 強制終了 (SIGKILL)",
		"3 該当ペインへ移動", "4 今回は無視 (15分スヌーズ)",
	}
	if a.StopDisabledReason != "" {
		actions[0], actions[1] = "", ""
	}
	return &render.Dialog{
		Detail:        a.StopDisabledReason,
		Title:         "暴走検知",
		RequiredLines: []string{a.Machine, fmt.Sprintf("s%d · p%d · PID %d", a.Workspace, a.Pane, a.PID)},
		Lines: []string{
			a.SpaceName,
			a.Name,
			a.Command,
			fmt.Sprintf("RSS %s (+%s / 30秒)", runawayMemory(a.RSS), runawayMemory(a.Spike)),
			fmt.Sprintf("ツリー RSS %s (+%s / 30秒)", runawayMemory(a.TreeRSS), runawayMemory(a.TreeSpike)),
			fmt.Sprintf("CPU %.1f%% · 高負荷 %d秒", a.CPUPercent, a.CPUSeconds),
		},
		Actions:           actions,
		CenterTerminal:    true,
		PreserveTitleCase: true,
	}
}

func (m *machineClient) runawayInput(event input.Event) bool {
	if event.Kind == input.KindFocus || event.Kind == input.KindBlur || event.Kind == input.KindTerminalReport {
		return false
	}
	click := event.Kind == input.KindMouseClick && event.Mouse.Button == uv.MouseLeft
	if click && m.regions != nil && m.runawayToast.id != "" && m.regions.ToastRect.Contains(event.Mouse.X, event.Mouse.Y) {
		m.suppressRelease = true
		m.inspectRunaway(m.runawayToast)
		return true
	}
	if m.runawayPainted.id == "" && m.runawayInspect.id == "" {
		return false
	}
	if event.Kind == input.KindMouseRelease && m.suppressRelease {
		m.suppressRelease = false
		return true
	}
	if click && m.runawayCanAct() {
		m.suppressRelease = true
		for i, r := range m.regions.DialogActions {
			if r.Contains(event.Mouse.X, event.Mouse.Y) {
				m.runawayChoose(i)
				break
			}
		}
	}
	if event.Kind == input.KindKey && event.Key.Mod == 0 {
		switch event.Key.Code {
		case uv.KeyEscape:
			m.runawayOpen, m.runawayInspect = runawaySelection{}, runawaySelection{}
			m.runawayFocus = nil
			m.runawayFocusError = ""
		case '1', '2', '3', '4', '5':
			index := int(event.Key.Code - '1')
			if m.runawayCanAct() && index < len(m.regions.DialogActions) && m.regions.DialogActions[index].W > 0 {
				m.runawayChoose(index)
			}
		}
	}
	return true
}

func (m *machineClient) runawayCanAct() bool {
	return m.regions != nil && m.runawayPainted.id != "" && m.runawayPainted == m.runawayOpen &&
		m.runawayPaintedSize == m.size && m.runawayInspect.id == "" && m.runawayFocus == nil
}

func (m *machineClient) runawayDisconnect(machine string) {
	if m.runawayInspect.machine == machine {
		m.runawayInspect = runawaySelection{}
	}
	if m.runawayFocus != nil && m.runawayFocus.selection.machine == machine {
		m.runawayFocus = nil
	}
	if m.runawayOpen.machine == machine {
		m.runawayOpen = runawaySelection{}
		m.runawayFocusError = ""
	}
}

func (m *machineClient) inspectRunaway(selection runawaySelection) {
	if _, ok := m.findRunaway(selection); !ok {
		return
	}
	e := m.runawayEndpoint(selection.machine)
	err := e.connection.enqueue(msg{Type: endpoint.Runaway,
		RunawayAction: &endpoint.RunawayAction{ID: selection.id, Kind: endpoint.RunawayInspect}})
	if err != nil {
		m.notify(err.Error())
		return
	}
	m.runawayInspect = selection
}

func (m *machineClient) finishRunawayInspect(machine string, response msg) {
	selection := runawaySelection{machine, response.RunawayAction.ID}
	if m.runawayInspect != selection {
		return
	}
	m.runawayInspect = runawaySelection{}
	if response.Reason == "" {
		if _, ok := m.findRunaway(selection); ok {
			m.runawayOpen = selection
			m.runawayFocusError = ""
		}
	}
}

func (m *machineClient) sendRunawayAction(selection runawaySelection, kind string) {
	a, ok := m.findRunaway(selection)
	if !ok {
		m.runawayOpen = runawaySelection{}
		return
	}
	e := m.runawayEndpoint(selection.machine)
	move := kind == endpoint.RunawayFocus && selection.machine != m.active
	if move {
		if err := e.connection.enqueue(resizeMessage(m.size)); err != nil {
			m.notify(err.Error())
			return
		}
	}
	err := e.connection.enqueue(msg{Type: endpoint.Runaway, RunawayAction: &endpoint.RunawayAction{ID: a.ID, Kind: kind}})
	if err != nil {
		m.notify(err.Error())
		return
	}
	if kind == endpoint.RunawayFocus {
		m.runawayFocus = &runawayFocusRequest{selection: selection, pane: a.Pane, workspace: a.Workspace}
		m.runawayFocusError = ""
	}
	// Keep the dialog through the focus response and through SIGTERM.
	if kind == endpoint.RunawaySnooze {
		m.runawayOpen = runawaySelection{}
	}
}

func (m *machineClient) finishRunawayFocus(machine string, response msg) {
	selection := runawaySelection{machine, response.RunawayAction.ID}
	request := m.runawayFocus
	if request == nil || request.selection != selection {
		return
	}
	m.runawayFocus = nil
	if response.Reason != "" {
		m.runawayFocusError = response.Reason
		return
	}
	m.runawayOpen = runawaySelection{}
	if e := m.runawayEndpoint(machine); e != nil && machine != m.active {
		m.pending = &pendingMachine{id: machine, workspace: request.workspace, pane: request.pane,
			deadline: time.Now().Add(machineSwitchWait), requested: true, inputSequence: e.connection.lastSentInput()}
		m.finishSwitch()
	}
}
