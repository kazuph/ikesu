package client

import "github.com/kazuph/ikesu/internal/benchstage"

import (
	"reflect"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/tui"
)

func (m *machineClient) paint() error {
	defer benchstage.Span("client.paint")()
	if m.inputBatch || m.outputFence.Waiting(time.Now()) {
		m.paintQueued = true
		return nil
	}
	m.paintQueued = false
	e := m.endpoints[m.active]
	if e.snapshot == nil {
		return nil
	}
	if err := m.syncKeyboardOutput(e); err != nil {
		return err
	}
	if err := m.setMouseMode(e.mouse.pixel); err != nil {
		return err
	}
	in := *e.snapshot
	in.Koi = true
	if m.settings != nil && m.settings.config != nil {
		in.Koi = m.settings.config.KoiEnabled()
	}
	in.KoiKitty = m.koiKitty
	m.mouse.Geometry(m.size)
	in.KoiCellW, in.KoiCellH = m.mouse.cellW, m.mouse.cellH
	if in.KoiCellW <= 0 || in.KoiCellH <= 0 {
		in.KoiKitty = false
	}
	in, nativePaint, err := m.nativePaintInput(in)
	if err != nil {
		return err
	}
	if err := m.inputSource.Prefix(in.Prefix, in.SidebarConfig.Experimental.SwitchASCIIInputSourceInPrefix); err != nil {
		m.notify("input source: " + err.Error())
	}
	if m.forms != nil {
		m.forms.Overlay(&in)
	}
	m.sidebar.PaneStatus = nil
	if e.connection == nil {
		m.sidebar.PaneStatus = []string{e.profile.Label + ": " + e.status, e.reason, "手元のspaceは使える"}
	}
	m.sidebar.Groups = m.sidebar.Groups[:0]
	m.sidebar.Active = m.active
	m.sidebar.DropMachine, m.sidebar.DropIndex = "", nil
	m.sidebar.DropSection = ""
	if m.drag != nil && m.drag.started {
		m.sidebar.DropMachine, m.sidebar.DropSection = m.drag.id, m.drag.targetSection
		if m.drag.drop >= 0 {
			m.sidebar.DropIndex = &m.drag.drop
		}
	}
	for _, id := range m.order {
		group := m.endpoints[id]
		input := group.snapshot
		if !group.profile.Enabled {
			input = nil
		}
		m.sidebar.Groups = append(m.sidebar.Groups, render.MachineGroup{ID: id, Label: group.profile.Label, Status: group.status, Reason: group.reason, Input: input})
	}
	m.koiClock.Apply(&in, &m.sidebar, time.Now(), tui.FossilExpiry())
	if m.koiInput.KoiKitty && in.KoiKitty {
		in.KoiTime = m.koiInput.KoiTime
	}

	var composed *render.Output
	if len(m.sidebar.Groups) == 1 && e.connection != nil {
		composed = m.composer.ComposeClient(in, nil, m.clientVersion, m.notice != "")
		m.sidebar.Hits = m.sidebar.Hits[:0]
	} else {
		composed = m.composer.ComposeClient(in, &m.sidebar, m.clientVersion, m.notice != "")
	}
	m.koiInput, m.koiScene = in, composed.Frame.KoiScene
	m.koiVisible = !m.blurred && composed.Frame.KoiActive
	if !m.koiVisible {
		m.koiNext = time.Time{}
	} else if m.koiNext.IsZero() {
		m.koiNext = time.Now().Add(render.KoiInterval)
	} // existing TUI tick interval and agreed 20fps
	m.drawTabFeedback(composed)
	f := composed.Frame
	if m.notice != "" {
		x, y, end := composed.ActionBar.Copy.X+composed.ActionBar.Copy.W, composed.ActionBar.Area.Y, composed.ActionBar.Cycle.X
		if end-x < len([]rune(m.notice)) {
			x = 1
			y = f.H - 1
			end = max(x, in.State.SidebarArea().W-1)
		}
		for _, r := range m.notice {
			if x >= end {
				break
			}
			if cell := f.Cell(x, y); cell != nil {
				style := cell.Style
				style.Fg = f.Palette.Text
				f.Set(x, y, string(r), style)
			}
			x++
		}
	}
	if in.SidebarConfig.UI.HostCursor == "drawn" && composed.AnchorVisible && composed.AnchorShape == 0 {
		if cell := f.Cell(composed.AnchorX, composed.AnchorY); cell != nil {
			cell.Style.Attrs |= uv.AttrReverse
			composed.AnchorVisible = false
		}
	}
	if f.W != m.size.Cols || f.H != m.size.Rows {
		clipped := f.Viewport(m.size.Cols, m.size.Rows)
		clipped.RetainGraphics = true
		composed.Frame = clipped
		composed.AnchorVisible = composed.AnchorVisible && composed.AnchorX < clipped.W && composed.AnchorY < clipped.H
	}
	composed.Frame.GraphicsNamespace = m.active
	var nativePanes map[int]*render.PaneSnap
	if local := m.endpoints["local"]; local != nil && local.snapshot != nil {
		nativePanes = local.snapshot.Panes
	}
	if m.active == "local" {
		nativePanes = in.Panes
	}
	var retired []byte
	if !composed.Frame.RetainGraphics {
		retired, err = m.frames.ObserveNativeImages(nativePanes)
		if err != nil {
			return err
		}
	}
	data := m.frames.Frame(composed.Frame, composed.AnchorX, composed.AnchorY, composed.AnchorVisible, composed.AnchorShape)
	data = append(data, retired...)
	endWrite := benchstage.Span("outer.write")
	_, writeErr := m.out.Write(data)
	endWrite()
	benchstage.Values("outer.bytes", len(data), 0)
	if err := writeErr; err != nil {
		return err
	}
	probe := false
	if m.frames.RequiresImageFence() {
		probe = m.outputFence.Written(data, time.Now())
	}
	if probe {
		if _, err := m.out.WriteString(render.TerminalStatusQuery); err != nil {
			return err
		}
	}
	m.title.session, m.title.groups = m.session, m.titleGroups()
	if err := m.title.painted(m.out, in); err != nil {
		return err
	}
	graphicsRetained := composed.Frame.RetainGraphics
	composed.Frame = nil
	if e.connection != nil && (e.requestAck || m.regions == nil || !reflect.DeepEqual(m.regions, composed)) {
		if err := e.connection.enqueue(msg{Type: "rendered", Rendered: composed, FocusEpoch: e.epoch}); err != nil {
			return err
		}
		e.requestAck = false
	}
	m.regions = composed
	if nativePaint && !graphicsRetained {
		if err := m.nativePainted(); err != nil {
			return err
		}
	}
	m.tracePaint(data, probe, in)
	held := m.held
	m.held = nil
	for _, event := range held {
		if err := m.input(event); err != nil {
			return err
		}
	}
	return nil
}
