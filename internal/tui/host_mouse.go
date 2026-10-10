package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"io"
)

// BeginHostMouse starts an attachment in cell mode until pixel support and
// cell geometry are known. Queries/replies use the normal input stream.
func (a *App) BeginHostMouse(xp, yp int) {
	a.BeginHostTheme()
	a.hostPixelMouse = false
	a.hostPixelSupported = false
	a.cellW = 0
	a.cellH = 0
	a.SetCellPixels(xp, yp)
	a.syncLayout()
	if a.sidebarConfig.UI.MouseCapture != nil && !*a.sidebarConfig.UI.MouseCapture {
		_, _ = io.WriteString(a.out, MouseOffSeq)
		return
	}
	_, _ = io.WriteString(a.out, "\x1b[?1016l\x1b[?1016$p\x1b[16t\x1b[14t")
}

func (a *App) hostMouseReport(report uv.Event) {
	beforeW, beforeH := a.cellW, a.cellH
	switch r := report.(type) {
	case uv.ModeReportEvent:
		if r.Mode == ansi.ModeMouseExtSgrPixel {
			a.hostPixelSupported = r.Value == ansi.ModeSet || r.Value == ansi.ModeReset || r.Value == ansi.ModePermanentlySet
		}
	case uv.CellSizeEvent:
		if r.Width > 0 && r.Height > 0 {
			a.cellW = r.Width
			a.cellH = r.Height
		}
	case uv.PixelSizeEvent:
		a.SetCellPixels(r.Width, r.Height)
	}
	if a.cellW != beforeW || a.cellH != beforeH {
		a.syncLayout()
	}
	if a.hostPixelMouse && !a.hostPixelSupported {
		_, _ = io.WriteString(a.out, "\x1b[?1016l")
		a.hostPixelMouse = false
	}
	if (a.sidebarConfig.UI.MouseCapture == nil || *a.sidebarConfig.UI.MouseCapture) && !a.hostPixelMouse && a.hostPixelSupported && a.cellW > 0 && a.cellH > 0 {
		if _, err := io.WriteString(a.out, "\x1b[?1016h"); err == nil {
			a.hostPixelMouse = true
		}
	}
}
