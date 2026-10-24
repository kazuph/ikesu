//go:build pprof

package runtime

import (
	"os"
	"sync"
)

var benchPanes struct {
	sync.Mutex
	panes []*Pane
}

func init() {
	if os.Getenv("IKESU_BENCH_STATS") == "" {
		return
	}
	benchPaneMonitor = func(p *Pane) { benchPanes.Lock(); benchPanes.panes = append(benchPanes.panes, p); benchPanes.Unlock() }
}
func BenchPaneStats() []map[string]any {
	benchPanes.Lock()
	panes := append([]*Pane(nil), benchPanes.panes...)
	benchPanes.Unlock()
	rows := make([]map[string]any, 0, len(panes))
	for _, p := range panes {
		p.mu.Lock()
		if p.emu != nil {
			rows = append(rows, map[string]any{"id": p.ID, "width": p.emu.Width(), "height": p.emu.Height(), "history_rows": p.emu.ScrollbackLen(), "history_bytes": p.emu.Scrollback().Bytes(), "dead": p.dead})
		}
		p.mu.Unlock()
	}
	return rows
}
