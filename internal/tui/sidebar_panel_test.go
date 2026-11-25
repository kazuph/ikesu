package tui

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/sidebarplugin"
	"github.com/kazuph/ikesu/internal/teststate"
)

// プラグインごとに見出しが付き、クリックしたプラグインの節だけが閉じたり開いたりする。
func TestSidebarPanelHeadingFolds(t *testing.T) {
	a := NewApp(model.New(t.TempDir(), 120, 40), runtime.NewSupervisor(), &bytes.Buffer{}, "")
	a.st.SidebarVisible = true
	a.panel = &sidebarPanelState{plugins: []sidebarplugin.Item{
		{File: "usage.30s.sh", Name: "usage", Lines: []string{"CC W  \x1b[31m██\x1b[0m 40%", "CX W  ----"}, Ran: true},
		{File: "tenki.1h.sh", Name: "tenki", Lines: []string{"sunny"}, Ran: true},
	}}
	a.layoutSidebarPanel()
	before := a.st.SidebarPanelRows
	if before != 5 {
		t.Fatalf("panel rows = %d", before)
	}
	if a.panel.columns(sidebarplugin.Left) <= 0 {
		t.Fatal("sidebar width not passed to plugins")
	}
	heads := render.SidebarPanelLayout(a.st, a.sidebarPanel(sidebarplugin.Left))
	if len(heads) != 2 || heads[0].Key != "usage.30s.sh" {
		t.Fatalf("headings = %+v", heads)
	}
	a.sidebarClick(uv.Mouse{X: heads[0].Rect.X + 2, Y: heads[0].Rect.Y, Button: uv.MouseLeft})
	if !a.panel.collapsed["usage.30s.sh"] || a.panel.collapsed["tenki.1h.sh"] {
		t.Fatal("heading did not fold only its plugin")
	}
	a.layoutSidebarPanel()
	if a.st.SidebarPanelRows != before-2 {
		t.Fatalf("folded rows = %d", a.st.SidebarPanelRows)
	}
	heads = render.SidebarPanelLayout(a.st, a.sidebarPanel(sidebarplugin.Left))
	a.sidebarClick(uv.Mouse{X: heads[0].Rect.X, Y: heads[0].Rect.Y, Button: uv.MouseLeft})
	if a.panel.collapsed["usage.30s.sh"] {
		t.Fatal("second click did not open it")
	}
}

// プラグインが無いときは、パネルを出さずスペースの欄を狭めない。
func TestSidebarPanelHiddenWithoutPlugins(t *testing.T) {
	a := NewApp(model.New(t.TempDir(), 120, 40), runtime.NewSupervisor(), &bytes.Buffer{}, "")
	a.st.SidebarVisible = true
	a.panel = &sidebarPanelState{}
	if p := a.layoutSidebarPanel(); p != nil || a.st.SidebarPanelRows != 0 {
		t.Fatalf("empty panel shown: %+v rows=%d", p, a.st.SidebarPanelRows)
	}
}

// パネルの行数は画面（ビューアー）ごとに覚え、切り替えても混ざらない。
func TestSidebarPanelRowsFollowEachViewer(t *testing.T) {
	s := model.New(t.TempDir(), 120, 40)
	s.SidebarPanelRows = 5
	d := captureDisplay(s)
	s.SidebarPanelRows = 0
	d.apply(s)
	if s.SidebarPanelRows != 5 {
		t.Fatalf("panel rows not restored: %d", s.SidebarPanelRows)
	}
}

// サイドバーを閉じて起動しても、最初の実行の前に開いたときの幅が決まっている。
func TestSidebarPanelWidthKnownBeforeFirstRun(t *testing.T) {
	a := NewApp(model.New(t.TempDir(), 120, 40), runtime.NewSupervisor(), &bytes.Buffer{}, "")
	a.st.SidebarVisible = false
	a.sidebarConfig.SetLoadedFromFile(true)
	done := make(chan struct{})
	defer close(done)
	a.startSidebarPanel(done)
	if got := a.panel.columns(sidebarplugin.Left); got <= 0 || got != a.st.ExpandedSidebarWidth()-2 {
		t.Fatalf("columns = %d", got)
	}
}

// 幅が続けて変わっても、間隔を空けたあと最後の幅で実行し直す。
func TestSidebarPanelRerunsForLatestWidth(t *testing.T) {
	a := NewApp(model.New(t.TempDir(), 120, 40), runtime.NewSupervisor(), &bytes.Buffer{}, "")
	a.panel = &sidebarPanelState{runner: &sidebarplugin.Runner{}}
	a.layoutSidebarPanel() // 最初の幅で1回
	first := a.panel.rerunAt
	a.st.RightSidebarSize = a.st.ExpandedRightSidebarWidth() + 4
	a.layoutSidebarPanel() // 右側の30秒以内の変更は保留
	if !a.panel.rerunAt.Equal(first) || !a.panel.rerunDue {
		t.Fatalf("width change inside the gap was not held: due=%v", a.panel.rerunDue)
	}
	a.panel.rerunAt = a.panel.rerunAt.Add(-widthRerunGap)
	a.rerunSidebarPlugins(time.Now()) // 再描画がなくても tick で実行する
	if a.panel.rerunDue || a.panel.rerunAt.Equal(first.Add(-widthRerunGap)) {
		t.Fatal("held width change was never run")
	}
}

func TestSidebarPanelOptInOnReload(t *testing.T) {
	teststate.Lock(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	teststate.Setenv(t, config.EnvVar, path)
	dir := sidebarplugin.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.30s.sh"), []byte("#!/bin/sh\nprintf 'real script output\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	a := NewApp(model.New(t.TempDir(), 120, 40), runtime.NewSupervisor(), &bytes.Buffer{}, "")
	defer close(a.doneCh)
	a.startSidebarPanel(a.doneCh)
	if a.panel.runner != nil || a.sidebarPanel(sidebarplugin.Left) != nil {
		t.Fatal("plugins started with a fresh config")
	}
	enabled := true
	var c config.Config
	c.UI.SidebarPlugins.Enabled = &enabled
	a.SetSidebarConfig(c, false)
	first := a.panel.runner
	if first == nil || a.panel.cancel == nil {
		t.Fatal("plugins did not start after enabling")
	}
	timeout := time.NewTimer(3 * time.Second)
	defer timeout.Stop()
	for len(a.panel.plugins) != 1 || !a.panel.plugins[0].Ran {
		select {
		case job := <-a.jobs:
			job.fn()
			close(job.done)
		case <-timeout.C:
			t.Fatal("enabled sidebar plugin did not run")
		}
	}
	if got := a.panel.plugins[0].Lines; len(got) != 1 || got[0] != "real script output" {
		t.Fatalf("unexpected script output: %v", got)
	}
	a.SetSidebarConfig(config.Config{}, false)
	if a.panel.runner != nil || a.panel.cancel != nil || a.sidebarPanel(sidebarplugin.Left) != nil {
		t.Fatal("disabled plugins still running or displayed")
	}
	a.SetSidebarConfig(c, false)
	if a.panel.runner == nil || a.panel.runner == first {
		t.Fatal("re-enabling did not start a new runner")
	}
	a.SetSidebarConfig(config.Config{}, false)
}

func TestRightSidebarPluginHeadingFoldsOnlyItsSection(t *testing.T) {
	a := NewApp(model.New(t.TempDir(), 160, 40), runtime.NewSupervisor(), &bytes.Buffer{}, "")
	a.st.SidebarVisible = true
	a.st.SidebarMin, a.st.SidebarSize, a.st.RightSidebarSize = 16, 26, 16
	a.jobsPanel = true
	a.panel = &sidebarPanelState{plugins: []sidebarplugin.Item{
		{File: "left/usage.30s.sh", Name: "Left", Position: sidebarplugin.Left, Lines: []string{"left body"}, Ran: true},
		{File: "right/usage.30s.sh", Name: "Right", Position: sidebarplugin.Right, Lines: []string{"right body"}, Ran: true},
		{File: "right/other.30s.sh", Name: "Other", Position: sidebarplugin.Right, Lines: []string{"other body"}, Ran: true},
	}}
	a.layoutSidebarPanel()
	if a.panel.columns(sidebarplugin.Left) != 24 || a.panel.columns(sidebarplugin.Right) != 14 {
		t.Fatal("plugin columns did not follow their side")
	}
	panel := a.sidebarPanel(sidebarplugin.Right)
	heads := render.SidebarPanelHeadings(panel, render.RightSidebarPanelRect(a.st, panel))
	if len(heads) != 2 {
		t.Fatalf("right headings = %+v", heads)
	}
	height := a.sidebarView().Agents.Rect.H
	a.click(uv.Mouse{X: heads[0].Rect.X + 1, Y: heads[0].Rect.Y, Button: uv.MouseLeft})
	if !a.panel.collapsed[heads[0].Key] || a.panel.collapsed[heads[1].Key] || a.panel.collapsed["left/usage.30s.sh"] {
		t.Fatalf("folded = %+v", a.panel.collapsed)
	}
	if got := a.sidebarView().Agents.Rect.H; got != height+1 {
		t.Fatalf("jobs viewport after folding = %d, want %d", got, height+1)
	}
	panel = a.sidebarPanel(sidebarplugin.Right)
	r := render.RightSidebarPanelRect(a.st, panel)
	a.click(uv.Mouse{X: r.X + 1, Y: r.Y, Button: uv.MouseLeft})
	if a.panel.collapsed[heads[0].Key] {
		t.Fatal("second click did not reopen right plugin")
	}
}
