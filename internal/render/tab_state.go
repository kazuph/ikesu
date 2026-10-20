package render

import "github.com/kazuph/ikesu/internal/model"

func paneTaskOrOSC(p *PaneSnap) string {
	if p.TaskTitle != "" {
		return p.TaskTitle
	}
	return p.Title
}

// tabGlyph uses the same attention order and pane glyphs as the workspace sidebar.
func tabGlyph(tab *model.Tab, in Input) StatusGlyph {
	best := StatusGlyph{Rune: " ", Color: Dim}
	for _, pid := range tab.Layout.PaneIDs() {
		if p := in.Panes[pid]; p != nil && glyphRank(p.Status) > glyphRank(best) {
			best = p.Status
		}
	}
	return best
}
