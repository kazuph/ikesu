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
	f.Fill(r.X, r.Y, r.W, r.H, " ", uv.Style{Fg: Text, Bg: SurfDim})
	f.Box(r.X, r.Y, r.W, r.H, uv.Style{Fg: Accent, Bg: SurfDim})
	if r.W > 4 {
		f.Text(r.X+1, r.Y, " popup ", uv.Style{Fg: Accent, Bg: SurfDim}, r.X+r.W-1)
	}
	pid := s.Popup.PaneID
	snap := in.Panes[pid]
	if snap == nil || r.W < 3 || r.H < 3 {
		return
	}
	x, y, w, h := r.X+1, r.Y+1, r.W-2, r.H-2
	f.CopyGrid(x, y, clipGrid(snap.Grid, w, h))
	if in.Sel != nil && in.Sel.Pane == pid {
		applySelection(f, x, y, w, h, in.Sel)
	}
	out.AnchorX = x + max(0, min(snap.Cursor.X, w-1))
	out.AnchorY = y + max(0, min(snap.Cursor.Y, h-1))
	out.AnchorVisible = snap.Cursor.Visible && snap.ScrollOff == 0
}
