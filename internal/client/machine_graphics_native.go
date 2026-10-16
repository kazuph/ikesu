package client

import (
	"time"

	"github.com/kazuph/goterrace/internal/render"
)

func hasNativeGraphics(in render.Input) bool {
	for _, pane := range in.Panes {
		for _, placement := range pane.Graphics {
			if placement.Image.NativeID != 0 {
				return true
			}
		}
	}
	return false
}

func (m *machineClient) retirePendingGraphicsFile() {
	if identity := m.fileGraphics.disconnected(time.Now()); identity != nil {
		_, _ = m.out.Write(m.frames.ReleaseNativeImage(identity.ImageID))
	}
}
