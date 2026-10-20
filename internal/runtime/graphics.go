package runtime

import (
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/ikesu/internal/kitty"
	"io"

	"golang.org/x/sys/unix"
)

// Native placement requires this viewer's exact cell and pane dimensions.
func (p *Pane) MatchesGraphicsGeometry(cols, rows, cw, ch int) bool {
	if cols <= 0 || rows <= 0 || cw <= 0 || ch <= 0 {
		return false
	}
	ws, err := unix.IoctlGetWinsize(p.ptyFD, unix.TIOCGWINSZ)
	return err == nil && int(ws.Col) == cols && int(ws.Row) == rows && int(ws.Xpixel) == cols*cw && int(ws.Ypixel) == rows*ch
}

// CellPixels reads the pane's own PTY geometry; zero means unavailable.
func (p *Pane) CellPixels() (int, int) {
	ws, err := unix.IoctlGetWinsize(p.ptyFD, unix.TIOCGWINSZ)
	if err != nil || ws.Col == 0 || ws.Row == 0 {
		return 0, 0
	}
	return int(ws.Xpixel) / int(ws.Col), int(ws.Ypixel) / int(ws.Row)
}

func (s *Supervisor) SetGraphicsVersion(version string) {
	s.mu.Lock()
	s.graphicsVersion = version
	s.mu.Unlock()
}

func (s *Supervisor) SetGraphics(enabled bool) {
	s.mu.Lock()
	s.graphicsEnabled = enabled
	panes := make([]*Pane, 0, len(s.panes))
	for _, p := range s.panes {
		panes = append(panes, p)
	}
	s.mu.Unlock()
	for _, p := range panes {
		p.mu.Lock()
		p.graphicsEnabled = enabled
		p.mu.Unlock()
	}
}

func (s *Supervisor) setupGraphics(p *Pane) {
	s.mu.Lock()
	p.graphicsEnabled, p.graphicsVersion = s.graphicsEnabled, s.graphicsVersion
	s.mu.Unlock()
	p.emu.RegisterCsiHandler('t', func(params ansi.Params) bool {
		op, _, _ := params.Param(0, 0)
		cw, ch := p.cellWidth, p.cellHeight
		if cw <= 0 || ch <= 0 {
			// Reference 0.2.57 answers queries with 8x16 when pixels are
			// unavailable. Keep this separate from actual mouse geometry.
			cw, ch = 8, 16
		}
		switch op {
		case 14:
			fmt.Fprintf(p.emu.InputPipe(), "\x1b[4;%d;%dt", p.emu.Height()*ch, p.emu.Width()*cw)
		case 16:
			fmt.Fprintf(p.emu.InputPipe(), "\x1b[6;%d;%dt", ch, cw)
		case 18:
			fmt.Fprintf(p.emu.InputPipe(), "\x1b[8;%d;%dt", p.emu.Height(), p.emu.Width())
		default:
			return false
		}
		return true
	})
	p.emu.RegisterCsiHandler(ansi.Command('>', 0, 'q'), func(params ansi.Params) bool {
		name := "libghostty"
		if p.graphicsEnabled {
			name = "ghostty (ikesu " + p.graphicsVersion + ")"
		}
		_, _ = io.WriteString(p.emu.InputPipe(), "\x1bP>|"+name+"\x1b\\")
		return true
	})
	p.emu.RegisterApcHandler(func(data []byte) bool {
		if len(data) == 0 || data[0] != 'G' {
			return false
		}
		if !p.graphicsEnabled {
			// No success response: disabled panes must not advertise images.
			response := kitty.Reject(data, "ENOTSUP:kitty graphics is disabled")
			if response != "" {
				_, _ = io.WriteString(p.emu.InputPipe(), response)
			}
			return true
		}
		if p.graphics == nil {
			p.graphics = &kitty.Store{}
			p.emu.SetScrollCallback(p.scrollGraphics)
		}
		position := p.emu.CursorPosition()
		response, cols, rows, handled := p.graphics.Handle(data, position.X, position.Y, p.cellWidth, p.cellHeight, p.altScreen)
		if response != "" {
			_, _ = io.WriteString(p.emu.InputPipe(), response)
		}
		if cols != 0 || rows != 0 {
			p.emu.AdvanceImageCursor(cols, rows)
		}
		return handled
	})
}

func (p *Pane) Graphics() []kitty.Placement {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.graphicsEnabled {
		return nil
	}
	var out []kitty.Placement
	if p.graphics != nil {
		out = p.graphics.Visible(p.altScreen)
	}
	return p.appendGraphicsLayers(out)
}

func (p *Pane) scrollGraphics(left, top, right, bottom, lines int) {
	p.graphics.Scroll(p.altScreen, left, top, right, bottom, lines)
}
