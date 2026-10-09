package tui

import (
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/teststate"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func menuIndex(a *App, label string) int {
	for i, item := range a.menu.m.Items {
		if item.Label == label {
			return i
		}
	}
	return -1
}

func TestG5MenuAgentPresetTargetsClickedPaneAndRegisters(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	if err := a.sup.WaitLoginPath(); err != nil {
		t.Fatal(err)
	}
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	var cfg config.Config
	cfg.AgentStart.Commands = map[string][]string{"codex": {"/bin/sh", "-c", "printf 'PRESET_STARTED'; exec sleep 60"}}
	a.SetSidebarConfig(cfg, false)
	a.openWorkspaceMenu(1, 1, 0)
	got := []string{}
	for _, item := range a.menu.m.Items[:3] {
		got = append(got, item.Label)
	}
	if !reflect.DeepEqual(got, []string{"New Claude Code agent", "New Codex agent", "New agy agent"}) {
		t.Fatal(got)
	}
	a.openPaneMenu(50, 5, 1)
	a.activateMenu(menuIndex(a, "New Codex agent"))
	tab := a.st.ActiveWorkspace().TabRef()
	if tab.Layout.Focused() != 4 {
		t.Fatal("new pane focus", tab.Layout.Focused())
	}
	root := tab.Layout.Root()
	if root.First == nil || root.First.First == nil || root.First.First.Leaf != 1 || root.First.Second.Leaf != 4 {
		t.Fatal("wrong split target", root)
	}
	p := a.sup.Get(4)
	if p == nil {
		t.Fatal("missing process")
	}
	specWaitText(t, p, "PRESET_STARTED")
	if !reflect.DeepEqual(p.SpawnArgv(), cfg.AgentStart.Commands["codex"]) {
		t.Fatal(p.SpawnArgv())
	}
	if agent, _, ok := a.Reported(4); !ok || agent != "codex" {
		t.Fatal("agent target not registered")
	}
}

func TestG5PaneMenuRootSideMovesAndSinglePaneExcluded(t *testing.T) {
	teststate.Lock(t)
	for _, tc := range []struct {
		label string
		dir   layout.Direction
		first bool
	}{
		{"Move to left split", layout.Vertical, true}, {"Move to right split", layout.Vertical, false},
		{"Move to upper split", layout.Horizontal, true}, {"Move to lower split", layout.Horizontal, false},
	} {
		a := specRuntimeApp(t)
		a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
		a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
		a.openPaneMenu(30, 8, 2)
		a.activateMenu(menuIndex(a, tc.label))
		l := a.st.ActiveWorkspace().TabRef().Layout
		root := l.Root()
		if root.Direction != tc.dir || l.Focused() != 2 {
			t.Fatal(tc, root, l.Focused())
		}
		target, other := root.Second, root.First
		if tc.first {
			target, other = root.First, root.Second
		}
		if target.Leaf != 2 || other.First.Leaf != 1 || other.Second.Leaf != 3 {
			t.Fatal("root order", tc, root)
		}
	}
	a := specRuntimeApp(t)
	a.openPaneMenu(30, 8, 1)
	for _, label := range []string{"Move to left split", "Move to right split", "Move to upper split", "Move to lower split", "Equalize pane sizes", "Cycle pane layout", "Rotate panes", "Rotate panes reverse"} {
		if menuIndex(a, label) >= 0 {
			t.Fatal("single pane item", label)
		}
	}
}

func TestReview1SlowLoginShellDoesNotBlockRightClick(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	shell := filepath.Join(t.TempDir(), "slowsh")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\ncase \"$*\" in *-lic*) /bin/sleep 4;; esac\nexec /bin/sh \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	teststate.Setenv(t, "SHELL", shell)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	var cfg config.Config
	cfg.AgentStart.Commands = map[string][]string{"codex": {"/bin/sh", "-c", "printf 'REVIEW1_MENU PATH=%s' \"$PATH\"; exec /bin/sleep 60"}}
	cfg.Agent.Env = map[string]string{"PATH": "/menu"}
	a.SetSidebarConfig(cfg, false)
	done := make(chan struct{})
	go func() { a.Run(); close(done) }()
	defer func() { a.Stop(); <-done; a.sup.KillAll() }()
	before := time.Now()
	a.Do(func() { a.openPaneMenu(1, 1, 1); a.activateMenu(menuIndex(a, "New Codex agent")) })
	if elapsed := time.Since(before); elapsed >= 200*time.Millisecond {
		t.Fatal("right click blocked", elapsed)
	}
	time.Sleep(300 * time.Millisecond)
	tick := time.Now()
	a.Do(func() {})
	if elapsed := time.Since(tick); elapsed >= 200*time.Millisecond {
		t.Fatal("app loop blocked", elapsed)
	}
	if err := a.sup.WaitLoginPath(); err != nil {
		t.Fatal(err)
	}
	var pid int
	deadline := time.Now().Add(time.Second)
	for {
		a.Do(func() { pid = a.focusedPane() })
		if pid != 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("menu launch did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	p := a.sup.Get(pid)
	if p == nil || pid == 1 {
		t.Fatal("agent not launched", pid)
	}
	specWaitText(t, p, "REVIEW1_MENU PATH=/menu")
}
