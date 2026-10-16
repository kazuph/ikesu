package client

import (
	"fmt"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/tui"
)

type hostMouseState struct {
	parser       *ansi.Parser
	pixel        bool
	cellW, cellH int
}

// Track actual mode writes, including CSI sequences split across messages.
func (m *hostMouseState) Written(data []byte) {
	if m.parser == nil {
		m.parser = ansi.NewParser()
		m.parser.SetHandler(ansi.Handler{HandleCsi: func(command ansi.Cmd, params ansi.Params) {
			if command.Prefix() != '?' || command.Intermediate() != 0 || command.Final() != 'h' && command.Final() != 'l' {
				return
			}
			params.ForEach(0, func(_ int, value int, _ bool) {
				if value == int(ansi.ModeMouseExtSgrPixel) {
					m.pixel = command.Final() == 'h'
				}
			})
		}})
	}
	for _, b := range data {
		m.parser.Advance(b)
	}
}

func (m *hostMouseState) Geometry(size tui.Winsize) {
	if size.Cols > 0 && size.Xpixel/size.Cols > 0 {
		m.cellW = size.Xpixel / size.Cols
	}
	if size.Rows > 0 && size.Ypixel/size.Rows > 0 {
		m.cellH = size.Ypixel / size.Rows
	}
}

func (m *hostMouseState) Report(report uv.Event, size tui.Winsize) {
	switch value := report.(type) {
	case uv.CellSizeEvent:
		if value.Width > 0 && value.Height > 0 {
			m.cellW, m.cellH = value.Width, value.Height
		}
	case uv.PixelSizeEvent:
		size.Xpixel, size.Ypixel = value.Width, value.Height
		m.Geometry(size)
	}
}

func (m *hostMouseState) CellEvent(event input.Event, size tui.Winsize) input.Event {
	if m.cellW == 0 || m.cellH == 0 {
		m.Geometry(size)
	}
	if m.pixel && m.cellW > 0 && m.cellH > 0 {
		switch event.Kind {
		case input.KindMouseClick, input.KindMouseRelease, input.KindMouseWheel, input.KindMouseMotion:
			event.Mouse.X /= m.cellW
			event.Mouse.Y /= m.cellH
		}
	}
	return event
}

func (m *machineClient) setMouseMode(pixel bool) error {
	if m.drag != nil || m.mouse.pixel == pixel {
		return nil
	}
	sequence := "\x1b[?1016l"
	if pixel {
		sequence = "\x1b[?1016h"
	}
	n, err := m.out.WriteString(sequence)
	m.mouse.Written([]byte(sequence[:n]))
	return err
}

func (m *machineClient) sidebarMousePosition(target *managedEndpoint, event input.Event, row int) (int, int, error) {
	x, y := event.Mouse.X+1, row+1
	if !target.mouse.pixel {
		return x, y, nil
	}
	if m.mouse.cellW == 0 || m.mouse.cellH == 0 {
		m.mouse.Geometry(m.size)
	}
	w, h := m.mouse.cellW, m.mouse.cellH
	if w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("unsupported sidebar pixel geometry")
	}
	offsetX, offsetY := w/2, h/2
	if m.mouse.pixel {
		var button, rawX, rawY int
		var final byte
		if _, err := fmt.Sscanf(string(event.Raw), "\x1b[<%d;%d;%d%c", &button, &rawX, &rawY, &final); err != nil {
			return 0, 0, fmt.Errorf("unsupported sidebar mouse encoding")
		}
		offsetX, offsetY = (rawX-1)%w, (rawY-1)%h
	}
	return (x-1)*w + offsetX + 1, row*h + offsetY + 1, nil
}
