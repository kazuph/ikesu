package runtime

import (
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/goterrace/internal/kitty"
	"io"
)

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
	p.emu.RegisterCsiHandler(ansi.Command('>', 0, 'q'), func(params ansi.Params) bool {
		name := "libghostty"
		if p.graphicsEnabled {
			name = "ghostty (gtx " + p.graphicsVersion + ")"
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
	if !p.graphicsEnabled || p.graphics == nil {
		return nil
	}
	return p.graphics.Visible(p.altScreen)
}
