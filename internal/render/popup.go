package render

import uv "github.com/charmbracelet/ultraviolet"

func drawPopup(f *Frame, in Input, out *Output) {
	s := in.State
	if !s.PopupVisible() {
		return
	}
	r := s.PopupRect()
	if r.W < 1 || r.H < 1 {
		return
	}
	bg := f.Palette.SurfDim
	if in.ServerUI {
		bg = f.Palette.PopupBG
	}
	f.Fill(r.X, r.Y, r.W, r.H, " ", uv.Style{Fg: f.Palette.Text, Bg: bg})
	f.Box(r.X, r.Y, r.W, r.H, uv.Style{Fg: f.Palette.Accent, Bg: bg})
	if r.W > 4 {
		title := s.Popup.Title
		if title == "" {
			title = "popup"
		}
		f.Text(r.X+1, r.Y, title, uv.Style{Fg: f.Palette.Accent, Bg: bg}, r.X+r.W-1)
	}
	pid := s.Popup.PaneID
	snap := in.Panes[pid]
	if snap == nil || r.W < 3 || r.H < 3 {
		return
	}
	x, y, w, h := r.X+1, r.Y+1, max(1, r.W-3), r.H-2
	f.CopyGrid(x, y, clipGrid(snap.Grid, w, h))
	if r.W > 3 {
		f.Fill(r.X+r.W-2, y, 1, h, " ", uv.Style{Bg: bg})
	}
	if in.Sel != nil && in.Sel.Pane == pid {
		applySelection(f, x, y, w, h, in.Sel)
	}
	out.AnchorX = x + max(0, min(snap.Cursor.X, w-1))
	out.AnchorY = y + max(0, min(snap.Cursor.Y, h-1))
	out.AnchorVisible = snap.Cursor.Visible && snap.ScrollOff == 0
	out.AnchorShape = 0
	if snap.ScrollOff == 0 && !snap.Cursor.Visible && revealIMECursor(in.SidebarConfig, snap.Agent) {
		out.AnchorVisible = true
		out.AnchorShape = imeCursorShape(in.SidebarConfig.Experimental.CJKIMECursorShape)
	}
}
