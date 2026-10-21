package client

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/tui"
)

// Only local tab feedback uses these cell coordinates. Raw input keeps the
// host's original units for the server, which owns the gesture and pane input.
type tabMouseCoordinates struct {
	parser        *ansi.Parser
	pixel         bool
	width, height int
}

func (m *tabMouseCoordinates) output(data []byte) {
	if m.parser == nil {
		m.parser = ansi.NewParser()
		m.parser.SetHandler(ansi.Handler{HandleCsi: func(cmd ansi.Cmd, params ansi.Params) {
			if cmd != ansi.Cmd(ansi.Command('?', 0, 'h')) && cmd != ansi.Cmd(ansi.Command('?', 0, 'l')) {
				return
			}
			params.ForEach(0, func(_, value int, _ bool) {
				if value == int(ansi.ModeMouseExtSgrPixel) {
					m.pixel = cmd == ansi.Cmd(ansi.Command('?', 0, 'h'))
				}
			})
		}})
	}
	m.parser.Parse(data)
}

func (m *tabMouseCoordinates) report(report uv.Event, size tui.Winsize) {
	switch r := report.(type) {
	case uv.CellSizeEvent:
		if r.Width > 0 && r.Height > 0 {
			m.width, m.height = r.Width, r.Height
		}
	case uv.PixelSizeEvent:
		if r.Width > 0 && r.Height > 0 && size.Cols > 0 && size.Rows > 0 {
			m.width, m.height = r.Width/size.Cols, r.Height/size.Rows
		}
	}
	if m.width == 0 && m.height == 0 && size.Cols > 0 && size.Rows > 0 {
		m.width, m.height = size.Xpixel/size.Cols, size.Ypixel/size.Rows
	}
}

func (m *tabMouseCoordinates) cells(event input.Event) input.Event {
	if m.pixel && m.width > 0 && m.height > 0 {
		event.Mouse.X /= m.width
		event.Mouse.Y /= m.height
	}
	return event
}
