package client

import (
	"context"
	"encoding/base64"
	"fmt"
	"github.com/kazuph/goterrace/internal/machine"
	"os"
	"sync"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/render"
	"golang.org/x/sys/unix"
)

func readMachineInput(in *os.File, ctx context.Context) (<-chan input.Event, func(), error) {
	fd, err := unix.Dup(int(in.Fd()))
	if err != nil {
		return nil, nil, err
	}
	unix.CloseOnExec(fd)
	reader := os.NewFile(uintptr(fd), "machine-terminal-input")
	wake, finish, err := os.Pipe()
	if err != nil {
		reader.Close()
		return nil, nil, err
	}
	wakeFD := int32(wake.Fd())
	events := make(chan input.Event)
	stopped := make(chan struct{})
	done := make(chan struct{})
	var once sync.Once
	stop := func() {
		once.Do(func() { close(stopped); finish.Close(); <-done; reader.Close(); wake.Close() })
	}
	go func() {
		defer close(done)
		defer close(events)
		var framer input.Framer
		buf := make([]byte, 64*1024)
		for {
			timeout := -1
			if framer.Pending() {
				timeout = input.IdleFlushMs
			}
			fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}, {Fd: wakeFD, Events: unix.POLLIN}}
			ready, err := unix.Poll(fds, timeout)
			if err == unix.EINTR {
				continue
			}
			if err != nil || fds[1].Revents != 0 {
				return
			}
			var decoded []input.Event
			if ready == 0 {
				decoded = framer.FlushTimeout()
			} else {
				n, err := unix.Read(fd, buf)
				if err == unix.EINTR {
					continue
				}
				if err != nil || n == 0 {
					return
				}
				decoded = framer.Push(buf[:n])
			}
			for _, event := range decoded {
				if event.Kind == input.KindUnknown && string(event.Raw) == "\x1b[200~" {
					continue
				}
				select {
				case events <- event:
				case <-ctx.Done():
					return
				case <-stopped:
					return
				}
			}
		}
	}()
	return events, stop, nil
}

func eventBytes(event input.Event) []byte {
	if event.Kind == input.KindPaste {
		return []byte("\x1b[200~" + event.Text + "\x1b[201~")
	}
	return event.Raw
}

func (m *machineClient) input(event input.Event) error {
	if m.outputFence.Accept(event.Raw) {
		if m.paintQueued {
			return m.paint()
		}
		return nil
	}
	if event.Kind == input.KindBlur {
		m.blurred = true
	} else if event.Kind == input.KindFocus {
		m.blurred = false
	}
	if m.holdPendingInput(event, time.Now()) {
		return nil
	}
	if m.regions == nil {
		if m.legacyLocal() {
			return m.sendInput("local", eventBytes(event))
		}
		m.held = append(m.held, event)
		return nil
	}
	activeView := m.endpoints[m.active]
	if m.active != "local" && event.Kind == input.KindKey && activeView.snapshot != nil && remoteImagePasteKey(activeView.snapshot.SidebarConfig, event.Key, activeView.snapshot.Prefix) {
		if activeView.snapshot.Prefix {
			if activeView.connection == nil || activeView.connection.writer.available < endpoint.NotificationGeneration {
				return fmt.Errorf("endpoint_action_unsupported: prefix remote image paste requires endpoint generation %d", endpoint.NotificationGeneration)
			}
			if err := activeView.connection.enqueue(msg{Type: "input", EndPrefix: true}); err != nil {
				return err
			}
		}
		return m.pasteClipboard()
	}
	if m.active != "local" && (event.Kind == input.KindKey || event.Kind == input.KindUnknown) && event.Key.Code == 'v' && event.Key.Mod == uv.ModSuper {
		if event.Kind == input.KindUnknown {
			return nil
		}
		return m.pasteClipboard()
	}
	if m.forms != nil && m.forms.Input(event, activeView.snapshot.State) {
		return m.paint()
	}
	if handled, err := m.tabDragInput(event); handled || err != nil {
		return err
	}
	if m.drag != nil && (event.Kind == input.KindMouseMotion || event.Kind == input.KindMouseRelease) {
		return m.dragInput(event)
	}
	if activeView.connection == nil && event.Kind == input.KindMouseClick && event.Mouse.Button == uv.MouseLeft {
		r := render.SidebarMenuRect(activeView.snapshot.State)
		if contains(r.X, r.Y, r.W, r.H, event.Mouse.X, event.Mouse.Y) {
			if err := m.forms.Open("menu", activeView.snapshot.State); err != nil {
				return err
			}
			return m.paint()
		}
	}
	if event.Kind == input.KindMouseRelease && m.suppressRelease {
		m.suppressRelease = false
		return nil
	}
	if event.Kind == input.KindMouseClick || event.Kind == input.KindMouseWheel || event.Kind == input.KindMouseMotion || event.Kind == input.KindMouseRelease {
		active := m.endpoints[m.active]
		// Overlay hit areas belong to the selected server and take precedence.
		if active.snapshot != nil && active.snapshot.Menu == nil && active.snapshot.Dialog == nil && active.snapshot.Help == nil {
			for _, hit := range m.sidebar.Hits {
				if !contains(hit.Rect.X, hit.Rect.Y, hit.Rect.W, hit.Rect.H, event.Mouse.X, event.Mouse.Y) {
					continue
				}
				if event.Kind == input.KindMouseWheel {
					step := 3
					if active.snapshot.State.WorkspaceDensity == "slim" && active.snapshot.SidebarConfig.UI.Sidebar.Spaces.Rows == nil {
						step = 2
					}
					if event.Mouse.Button == uv.MouseWheelUp {
						m.sidebar.Scroll -= step
					} else {
						m.sidebar.Scroll += step
					}
					return m.paint()
				}
				if (hit.Header || hit.New) && hit.Machine != "local" && event.Kind == input.KindMouseClick && event.Mouse.Button == uv.MouseRight {
					m.forms.OpenMachine(m.endpoints[hit.Machine].profile, active.snapshot.State, event.Mouse.X, event.Mouse.Y)
					return m.paint()
				}
				if hit.New && event.Kind == input.KindMouseClick && event.Mouse.Button == uv.MouseLeft {
					m.suppressRelease = true
					return m.createWorkspace(hit.Machine)
				}
				if hit.Header && event.Kind == input.KindMouseClick {
					if !m.endpoints[hit.Machine].profile.Enabled {
						m.suppressRelease = true
						return nil
					}
					if e := m.endpoints[hit.Machine]; e.connection == nil {
						if e.approval != "" || e.status == "awaiting approval" || e.status == machine.ApprovalStopped {
							if err := machine.ClearApproval(e.profile.ID); err != nil {
								return err
							}
							e.approval = ""
						}
						e.retry.retryNow(time.Now())
						m.advance(time.Now())
					}
					m.suppressRelease = true
					return m.paint()
				}
				if hit.Workspace != 0 && event.Kind == input.KindMouseClick && event.Mouse.Button == uv.MouseLeft {
					m.suppressRelease = true
					e := m.endpoints[hit.Machine]
					selected := hit.Machine == m.active && e.snapshot.State.ActiveWorkspace().ID == hit.Workspace
					m.drag = &machineDrag{id: hit.Machine, workspace: hit.Workspace, source: hit.Row.Ws, pressHit: hit, pressEvent: event, selected: selected, x: event.Mouse.X, y: event.Mouse.Y, drop: -1, section: e.snapshot.State.Workspaces[hit.Row.Ws].SidebarSection()}
					if !selected {
						m.pending = &pendingMachine{id: hit.Machine, workspace: hit.Workspace, deadline: time.Now().Add(machineSwitchWait)}
						if e.connection == nil && hit.Machine == "local" {
							m.start(hit.Machine, nil)
						}
						m.requestPending()
					}
					return nil
				}
				return m.sidebarInput(hit, event)
			}
		}
	}
	return m.sendInput(m.active, eventBytes(event))
}

func (m *machineClient) createWorkspace(id string) error {
	e := m.endpoints[id]
	if e.connection == nil || e.snapshot == nil {
		return fmt.Errorf("machine %s is not connected", e.profile.Label)
	}
	rect := render.SidebarNewRect(e.snapshot.State)
	data := []byte(fmt.Sprintf("\x1b[<0;%d;%dM\x1b[<0;%d;%dm", rect.X+1, rect.Y+1, rect.X+1, rect.Y+1))
	if id != m.active {
		previous := map[int]bool{}
		for _, ws := range e.snapshot.State.Workspaces {
			previous[ws.ID] = true
		}
		m.pending = &pendingMachine{id: id, deadline: time.Now().Add(machineSwitchWait), create: true, previousWorkspaces: previous, requested: true}
		if err := e.connection.enqueue(resizeMessage(m.size)); err != nil {
			return err
		}
	}
	return m.sendInput(id, data)
}

func contains(x, y, w, h, px, py int) bool { return px >= x && py >= y && px < x+w && py < y+h }

func (m *machineClient) sendInput(id string, data []byte) error {
	e := m.endpoints[id]
	if id == "local" && e.restarting {
		e.restartInput = append(e.restartInput, data...)
		return nil
	}
	if e.paste != nil {
		e.paste.held = append(e.paste.held, clipboardItem{data: append([]byte(nil), data...)})
		return nil
	}
	if e.connection == nil {
		return nil
	}
	return e.connection.enqueue(msg{Type: "input", Data: base64.StdEncoding.EncodeToString(data)})
}

func (m *machineClient) sidebarInput(hit render.MachineHit, event input.Event) error {
	data, err := m.sidebarBytes(hit, event)
	if err != nil {
		return err
	}
	return m.sendInput(hit.Machine, data)
}

func (m *machineClient) sidebarBytes(hit render.MachineHit, event input.Event) ([]byte, error) {
	e := m.endpoints[hit.Machine]
	if e.connection == nil || e.snapshot == nil {
		return nil, nil
	}
	view := render.SidebarViewForInput(*e.snapshot)
	for _, position := range view.Rows {
		if position.Row != hit.Row {
			continue
		}
		var button, x, y int
		var final byte
		if _, err := fmt.Sscanf(string(event.Raw), "\x1b[<%d;%d;%d%c", &button, &x, &y, &final); err != nil {
			return nil, fmt.Errorf("unsupported sidebar mouse encoding")
		}
		return []byte(fmt.Sprintf("\x1b[<%d;%d;%d%c", button, x, position.Y+1, final)), nil
	}
	return nil, fmt.Errorf("unsupported sidebar row outside endpoint viewport")
}

func remoteImagePasteKey(c config.Config, key uv.Key, prefix bool) bool {
	for _, binding := range c.ActionBindings("remote_image_paste") {
		chord, err := config.ParseChord(binding)
		if err == nil && chord.Match(key, prefix) {
			return true
		}
	}
	return false
}
