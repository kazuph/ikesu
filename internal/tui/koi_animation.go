package tui

import (
	"github.com/kazuph/ikesu/internal/render"
	"time"
)

// Standalone owns the same local decoration clock as a connected presenter.
// The shared server and viewers never schedule this deadline.
func (a *App) paintKoi(now time.Time) {
	if a.snapshotSink != nil || a.drawViewers != nil || a.windowBlurred || !a.sidebarConfig.KoiEnabled() {
		a.koiNext = time.Time{}
		return
	}
	in := a.koiInput
	a.koiClock.Apply(&in, nil, now, fossilExpiry)
	frame := render.AnimateKoi(a.koiScene, in)
	a.koiInput = in
	data := a.enc.KoiFrame(frame, a.koiAnchor.AnchorX, a.koiAnchor.AnchorY, a.koiAnchor.AnchorVisible)
	if len(data) > 0 {
		if _, err := a.out.Write(data); err != nil {
			a.dirty = true
		}
	}
	a.koiLocalActive = frame.KoiActive
	a.koiNext = time.Time{}
	if a.koiLocalActive {
		a.koiNext = now.Add(frame.KoiScene.UpdateInterval())
	}
}
