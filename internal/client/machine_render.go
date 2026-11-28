package client

import "github.com/kazuph/ikesu/internal/benchstage"

import (
	"fmt"
	"reflect"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/cellwidth"
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
	in.LegacySidebarControls = e.connection != nil && legacySidebarControls(e.connection.writer)
	in.SidebarMenuRight = e.connection != nil && sidebarMenuRight(e.connection.writer)
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
	if m.idle != nil {
		in.Dialog, in.Menu = m.idle.dialog(m.size.Cols), nil
	}
	if m.prune != nil {
		in.Dialog, in.Menu = m.prune.dialog.Render(m.pruneArea()), nil
	}
	if m.idleToast != "" {
		in.Toast = &render.Toast{Title: m.idleToast}
	}
	if m.remoteProbeToast != nil {
		in.Toast = m.remoteProbeToast
	}
	if m.approvalCanOpen(e) {
		e.guardApproval.focus(fmt.Sprintf("p%d", e.snapshot.State.FocusedPane()))
	} else {
		e.guardApproval.dismiss()
		if e.snapshot.State != nil {
			e.guardApproval.pane = fmt.Sprintf("p%d", e.snapshot.State.FocusedPane())
		}
	}
	e.guardApproval.overlay(&in)
	runawayToast, runawayDialog := m.overlayRunaway(&in)
	m.sidebar.PaneStatus = nil
	if e.connection == nil {
		m.sidebar.PaneStatus = []string{e.profile.Label + ": " + e.status, e.reason, "手元のspaceは使える"}
	}
	m.sidebar.Groups = m.sidebar.Groups[:0]
	m.sidebar.Active = m.active
	m.sidebar.Oyagoi = nil
	if m.oyagoi != nil {
		m.sidebar.Oyagoi = m.oyagoiForSidebar()
	}
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
		reason := group.reason
		if group.status == "awaiting approval" {
			reason = group.approval
		}
		m.sidebar.Groups = append(m.sidebar.Groups, render.MachineGroup{ID: id, Label: group.profile.Label, Status: group.status, Reason: reason, Input: input})
	}
	m.koiClock.ApplyFrame(&in, m.koiInput, &m.sidebar, time.Now(), tui.FossilExpiry())

	var composed *render.Output
	if len(m.sidebar.Groups) == 1 && e.connection != nil && m.sidebar.Oyagoi == nil {
		composed = m.composer.ComposeClient(in, nil, m.clientVersion, m.notice != "")
		m.sidebar.Hits = m.sidebar.Hits[:0]
	} else {
		composed = m.composer.ComposeClient(in, &m.sidebar, m.clientVersion, m.notice != "")
	}
	m.koiInput, m.koiScene = in, composed.Frame.KoiScene
	m.koiRetained = composed.Frame.W != m.size.Cols || composed.Frame.H != m.size.Rows
	m.koiVisible = !m.koiRetained && !m.blurred && composed.Frame.KoiActive
	if !m.koiVisible {
		m.koiNext = time.Time{}
	} else if m.koiNext.IsZero() {
		m.koiNext = time.Now().Add(composed.Frame.KoiScene.UpdateInterval())
	} // existing TUI tick interval and agreed 20fps
	if runawayDialog.id == "" {
		m.drawTabFeedback(composed)
	}
	m.drawWorkspaceSearchUI(composed, in)
	f := composed.Frame
	if m.notice != "" {
		x, y, end := composed.ActionBar.Copy.X+composed.ActionBar.Copy.W, composed.ActionBar.Area.Y, composed.ActionBar.Cycle.X
		text := noticeDisplayText(m.notice)
		width := cellwidth.String(text)
		if end-x < width {
			x = 1
			y = f.H - 1
			end = max(x, in.State.SidebarArea().W-1)
			if menu := render.SidebarMenuRect(in.State, in.ServerUI); !in.LegacySidebarControls && menu.W > 0 {
				y = render.SidebarFooter(in.State).Y
			}
		}
		if available := end - x; available > 0 && width > available && len(text) != utf8.RuneCountInString(text) {
			const ellipsis = "…"
			prefixWidth := available - cellwidth.String(ellipsis)
			prefix := cellwidth.Cut(text, 0, prefixWidth)
			text = prefix + strings.Repeat(" ", prefixWidth-cellwidth.String(prefix)) + ellipsis
		}
		for text != "" {
			r, size := utf8.DecodeRuneInString(text)
			width := cellwidth.Scalar(r)
			if width == 0 {
				text = text[size:]
				continue
			}
			if x+width > end {
				break
			}
			for size < len(text) {
				next, n := utf8.DecodeRuneInString(text[size:])
				if cellwidth.Scalar(next) != 0 {
					break
				}
				size += n
			}
			if cell := f.Cell(x, y); cell != nil && (runawayDialog.id == "" ||
				!composed.DialogRect.Contains(x, y) && !composed.DialogRect.Contains(x+width-1, y)) {
				style := cell.Style
				style.Fg = f.Palette.Text
				f.Set(x, y, text[:size], style)
			}
			text = text[size:]
			x += width
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
	enabled := m.settings != nil && m.settings.config != nil && m.settings.config.UI.NativeMenus
	panes := m.nativePanes.Next(enabled, in, m.size.Cols, m.size.Rows, m.search != nil)
	data = append(panes, data...)
	data = append(data, retired...)
	endWrite := benchstage.Span("outer.write")
	_, writeErr := m.out.Write(data)
	endWrite()
	benchstage.Values("outer.bytes", len(data), 0)
	if err := writeErr; err != nil {
		m.nativePanes.Forget()
		return err
	}
	if err := m.writeNativeMenu(in); err != nil {
		return err
	}
	e.guardApproval.painted = ""
	r := composed.ApprovalRect
	if in.Approval != nil && r.W > 0 && r.H > 0 && r.X >= 0 && r.Y >= 0 && r.X+r.W <= m.size.Cols && r.Y+r.H <= m.size.Rows {
		e.guardApproval.painted = e.guardApproval.open
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
	m.rememberSelection(m.active, in)
	graphicsRetained := composed.Frame.RetainGraphics
	composed.Frame = nil
	if e.connection != nil && (e.requestAck || m.regions == nil || !reflect.DeepEqual(m.regions, composed)) {
		if err := e.connection.enqueue(msg{Type: "rendered", Rendered: composed, FocusEpoch: e.epoch}); err != nil {
			return err
		}
		e.requestAck = false
	}
	m.regions = composed
	m.runawayToast, m.runawayPainted = runawayToast, runawayDialog
	m.runawayPaintedSize = m.size
	m.reportClientView(in)
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

func noticeDisplayText(text string) string {
	var display strings.Builder
	display.Grow(len(text))
	previousSpace := false
	for len(text) > 0 {
		r, size := utf8.DecodeRuneInString(text)
		rawC1 := size == 1 && text[0] >= 0x80 && text[0] <= 0x9f
		text = text[size:]
		if rawC1 || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			r = ' '
		}
		space := r == ' '
		if !space || !previousSpace {
			display.WriteRune(r)
		}
		previousSpace = space
	}
	return display.String()
}

// writeNativeMenu tells a host app ([ui] native_menus) what menu or dialog
// this screen now shows; unchanged frames and disabled screens write nothing.
func (m *machineClient) writeNativeMenu(in render.Input) error {
	enabled := m.settings != nil && m.settings.config != nil && m.settings.config.UI.NativeMenus
	signal := m.nativeMenu.Next(enabled, in.Menu, in.Dialog)
	if signal == nil {
		return nil
	}
	if _, err := m.out.Write(signal); err != nil {
		m.nativeMenu.Forget()
		return err
	}
	return nil
}
