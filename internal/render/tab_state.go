package render

import "github.com/kazuph/goterrace/internal/model"

func paneTaskOrOSC(p *PaneSnap) string {
	if p.TaskTitle != "" {
		return p.TaskTitle
	}
	return p.Title
}

// Tab attention is intentionally distinct from the sidebar's completion order.
func tabMarker(tab *model.Tab, panes map[int]*PaneSnap) string {
	rank := 0
	marker := " "
	for _, pid := range tab.Layout.PaneIDs() {
		p := panes[pid]
		if p == nil {
			continue
		}
		r, m := 0, " "
		switch p.Status.Key {
		case "blocked":
			r, m = 5, "!"
		case "working":
			r, m = 4, ">"
		case "done":
			r, m = 3, "+"
		case "unknown":
			r = 2
		case "idle":
			r = 1
		}
		if r > rank {
			rank, marker = r, m
		}
	}
	return marker
}
