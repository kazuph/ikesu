package client

import (
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/tui"
	"time"
)

func (m *machineClient) paintKoi(now time.Time) error {
	if !m.koiInput.KoiKitty {
		return m.paint()
	}
	if m.outputFence.Waiting(now) {
		m.paintQueued = true
		return nil
	}
	in := m.koiInput
	m.koiClock.Apply(&in, &m.sidebar, now, tui.FossilExpiry())
	frame := render.AnimateKoi(m.koiScene, in)
	m.koiInput = in
	data := m.frames.KoiFrame(frame, m.regions.AnchorX, m.regions.AnchorY, m.regions.AnchorVisible)
	m.koiVisible = !m.blurred && frame.KoiActive
	m.koiNext = time.Time{}
	if m.koiVisible {
		m.koiNext = now.Add(render.KoiInterval)
	}
	if len(data) == 0 {
		return nil
	}
	if m.outputFence.Written(data, now) {
		data = append(data, render.TerminalStatusQuery...)
	}
	_, err := m.out.Write(data)
	return err
}
