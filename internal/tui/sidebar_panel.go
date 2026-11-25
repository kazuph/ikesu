package tui

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/sidebarplugin"
)

// sidebarPanelState holds the sidebar plugins' outputs shown at the bottom of
// both sidebars, and which plugin sections are folded. It is shared by
// every viewer of the App.
type sidebarPanelState struct {
	done                      <-chan struct{}
	cancel                    context.CancelFunc
	plugins                   []sidebarplugin.Item
	collapsed                 map[string]bool // by path relative to the plugin directory
	leftColumns, rightColumns atomic.Int64
	runner                    *sidebarplugin.Runner
	rerunAt                   time.Time // the last re-run for a width change
	rerunDue                  bool      // a width change waits for widthRerunGap to pass
}

// widthRerunGap keeps viewers of different widths (a PC and the phone) from
// re-running the plugins on every frame.
const widthRerunGap = 30 * time.Second

// startSidebarPanel starts running the sidebar plugins until done closes. It
// can be restarted when the config enables them again.
func (a *App) startSidebarPanel(done <-chan struct{}) {
	if a.panel == nil {
		a.panel = &sidebarPanelState{}
	}
	panel := a.panel
	panel.done = done
	if panel.runner != nil || !a.sidebarConfig.SidebarPluginsEnabled() {
		return
	}
	// 最初の実行の前に幅を決める（サイドバーを閉じて起動しても、開いたときの幅を渡す）。
	panel.updateColumns(a.st)
	ctx, cancel := context.WithCancel(context.Background())
	panel.cancel = cancel
	go func() {
		select {
		case <-done:
			cancel()
		case <-ctx.Done():
		}
	}()
	runner := &sidebarplugin.Runner{Dir: sidebarplugin.Dir(config.Path()), Columns: panel.columns}
	// 結果は画面に反映する時点で読む。通知が前後しても、古い結果で新しい結果を上書きしない。
	runner.OnChange = func() {
		a.Do(func() {
			if panel.runner == runner {
				panel.plugins = runner.Items()
				a.dirty = true
			}
		})
	}
	panel.runner = runner
	go runner.Run(ctx)
}

func (a *App) updateSidebarPanel() {
	panel := a.panel
	if panel == nil || panel.done == nil {
		return
	}
	if a.sidebarConfig.SidebarPluginsEnabled() {
		a.startSidebarPanel(panel.done)
		return
	}
	if panel.cancel != nil {
		panel.cancel()
		panel.cancel = nil
		panel.runner = nil
		panel.plugins = nil
		panel.rerunDue = false
		panel.rerunAt = time.Time{}
		a.dirty = true
	}
}

// sidebarPanel builds the render input for one side.
func (a *App) sidebarPanel(position sidebarplugin.Position) *render.SidebarPanel {
	if a.panel == nil || len(a.panel.plugins) == 0 {
		return nil
	}
	if position == sidebarplugin.Right && a.viewerContext != nil && !a.viewerContext.rightSidebarPlugins {
		return nil
	}
	p := &render.SidebarPanel{}
	for _, it := range a.panel.plugins {
		if (it.Position == sidebarplugin.Right) != (position == sidebarplugin.Right) {
			continue
		}
		sec := render.SidebarPanelSection{Key: it.File, Title: it.Name, Collapsed: a.panel.collapsed[it.File]}
		switch {
		case !it.Ran:
			sec.Lines = []render.SidebarPanelLine{{Text: "…", Error: true}}
		case it.Err != "" && len(it.Lines) == 0:
			sec.Lines = []render.SidebarPanelLine{{Text: it.Err, Error: true}}
		case len(it.Lines) == 0:
			sec.Lines = []render.SidebarPanelLine{{Text: "（出力なし）", Error: true}}
		default:
			for _, line := range it.Lines {
				sec.Lines = append(sec.Lines, render.SidebarPanelLine{Text: line})
			}
		}
		p.Sections = append(p.Sections, sec)
	}
	if len(p.Sections) == 0 {
		return nil
	}
	return p
}

func (p *sidebarPanelState) columns(position sidebarplugin.Position) int {
	if position == sidebarplugin.Right {
		return int(p.rightColumns.Load())
	}
	return int(p.leftColumns.Load())
}

func (p *sidebarPanelState) updateColumns(s *model.AppState) bool {
	left, right := s.ExpandedSidebarWidth(), s.ExpandedRightSidebarWidth()
	if width := s.SidebarArea().W; s.SidebarVisible && width > 1 {
		left = width
	}
	if width := s.RightSidebarArea().W; width > 1 && !s.RightSidebarCompact() {
		right = width
	}
	l, r := int64(max(0, left-2)), int64(max(0, right-2))
	leftChanged := p.leftColumns.Swap(l) != l
	rightChanged := p.rightColumns.Swap(r) != r
	return leftChanged || rightChanged
}

// layoutSidebarPanel decides the panel's rows for this frame.
func (a *App) layoutSidebarPanel() *render.SidebarPanel {
	p := a.sidebarPanel(sidebarplugin.Left)
	a.st.SidebarPanelRows = 0
	if a.panel != nil && a.panel.updateColumns(a.st) {
		a.panel.rerunDue = true
		a.rerunSidebarPlugins(time.Now())
	}
	if p == nil || !a.st.SidebarVisible {
		return p
	}
	area := a.st.SidebarArea()
	a.st.SidebarPanelRows = render.SidebarPanelRows(p, max(0, area.H-5))
	return p
}

// A second width change inside the gap waits for a tick, even when no
// further changes mark the view dirty.
func (a *App) rerunSidebarPlugins(now time.Time) {
	p := a.panel
	if p == nil || !p.rerunDue || p.runner == nil || now.Sub(p.rerunAt) < widthRerunGap {
		return
	}
	p.rerunDue = false
	p.rerunAt = now
	p.runner.RerunAll()
}

// sidebarPanelClick folds or opens a plugin's section when its heading is clicked.
func (a *App) sidebarPanelClick(x, y int) bool {
	var headings []render.SidebarPanelHeading
	if pointIn(x, y, a.st.RightSidebarArea()) {
		panel := a.sidebarPanel(sidebarplugin.Right)
		headings = render.SidebarPanelHeadings(panel, render.RightSidebarPanelRect(a.st, panel))
	} else {
		headings = render.SidebarPanelLayout(a.st, a.sidebarPanel(sidebarplugin.Left))
	}
	for _, h := range headings {
		if !pointIn(x, y, h.Rect) || strings.TrimSpace(h.Key) == "" {
			continue
		}
		if a.panel.collapsed == nil {
			a.panel.collapsed = map[string]bool{}
		}
		a.panel.collapsed[h.Key] = !a.panel.collapsed[h.Key]
		a.dirty = true
		return true
	}
	return false
}
