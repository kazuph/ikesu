package tui

import (
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/model"
	"github.com/kazuph/goterrace/internal/teststate"
	"testing"
)

func TestConfigPrefixAndKittyWorkspaceHistory(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	cfg, err := config.Decode([]byte(`[keys]
prefix="ctrl+t"
switch_workspace="cmd+1..9"
next_workspace="cmd+j"
previous_workspace="cmd+k"
focus_history_back="cmd+["
focus_history_forward="cmd+]"
`))
	if err != nil || len(cfg.Warnings) != 0 {
		t.Fatal(cfg.Warnings, err)
	}
	a.SetSidebarConfig(cfg, false)
	a.exec(model.CmdNewWorkspace{Name: "second", Cwd: a.cwd})
	a.exec(model.CmdNewWorkspace{Name: "third", Cwd: a.cwd})
	specFeed(a, "\x1b[49;9u")
	if a.st.Active != 0 {
		t.Fatal("cmd+1", a.st.Active)
	}
	specFeed(a, "\x1b[106;9u")
	if a.st.Active != 1 {
		t.Fatal("cmd+j", a.st.Active)
	}
	specFeed(a, "\x1b[107;9u")
	if a.st.Active != 0 {
		t.Fatal("cmd+k", a.st.Active)
	}
	specFeed(a, "\x1b[91;9u")
	if a.st.Active != 1 {
		t.Fatal("cmd+[", a.st.Active)
	}
	specFeed(a, "\x1b[93;9u")
	if a.st.Active != 0 {
		t.Fatal("cmd+]", a.st.Active)
	}
	specFeed(a, "\x14s")
	if a.settings == nil {
		t.Fatal("ctrl+t prefix not used")
	}
}
func TestConfigCommandsActualPTYAndPopupOwner(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	cfg, err := config.Decode([]byte(`[keys]
prefix="ctrl+t"
next_workspace="cmd+j"
previous_workspace="cmd+k"
[[keys.command]]
key="prefix+g"
type="pane"
command="printf PANE_COMMAND; exec /bin/cat"
[[keys.command]]
key="prefix+t"
type="popup"
command="printf POPUP_COMMAND; exec /bin/cat"
description="scratch"
width="80%"
height="80%"
`))
	if err != nil || len(cfg.Warnings) != 0 {
		t.Fatal(cfg.Warnings, err)
	}
	a.SetSidebarConfig(cfg, false)
	root := a.focusedPane()
	specFeed(a, "\x14g")
	if !a.st.OverlayVisible() || a.navigator != nil {
		t.Fatal("pane command did not replace goto")
	}
	specWaitText(t, a.sup.Get(a.focusedPane()), "PANE_COMMAND")
	a.closeFocusedPane()
	if a.focusedPane() != root || a.st.Overlay != nil {
		t.Fatal("temporary pane did not restore focus")
	}
	specFeed(a, "\x14t")
	if !a.st.PopupVisible() {
		t.Fatal("popup command")
	}
	pop := a.st.Popup.PaneID
	specWaitText(t, a.sup.Get(pop), "POPUP_COMMAND")
	if a.st.Popup.Width.Percent != 80 || a.st.Popup.Height.Percent != 80 {
		t.Fatal(a.st.Popup)
	}
	a.exec(model.CmdNewWorkspace{Name: "other", Cwd: a.cwd})
	if a.st.PopupVisible() || a.sup.Get(pop) == nil {
		t.Fatal("popup owner lifetime")
	}
	specFeed(a, "\x1b[107;9u")
	if !a.st.PopupVisible() {
		t.Fatal("return to owner")
	}
	specFeed(a, "\x1b")
	for a.fr.Pending() {
		for _, ev := range a.fr.FlushTimeout() {
			a.onInput(ev)
		}
	}
	if a.st.PopupVisible() || a.sup.Get(pop) != nil {
		t.Fatal("popup escape cleanup")
	}
}
