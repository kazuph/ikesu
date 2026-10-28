package tui

import (
	"encoding/json"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/teststate"
	"strings"
	"testing"
)

func tabDefaultApp(t *testing.T, body string) *App {
	t.Helper()
	a := specRuntimeApp(t)
	c, err := config.Decode([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	// Presentation crosses the same JSON config boundary on machine endpoints.
	wire, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var transported config.Config
	if err := json.Unmarshal(wire, &transported); err != nil {
		t.Fatal(err)
	}
	a.SetSidebarConfig(transported, false)
	if _, err := a.sup.SpawnWith(a.focusedPane(), a.cwd, 80, 20, nil, []string{"/bin/sh"}); err != nil {
		t.Fatal(err)
	}
	a.exec(model.CmdNewTab{Workspace: 0})
	a.exec(model.CmdNewTab{Workspace: 0})
	a.exec(model.CmdSelectTab{Workspace: 0, Tab: 0})
	return a
}

func TestTabDefaultsRawPressRepeatReleaseWrapAndLegacyPrefix(t *testing.T) {
	teststate.Lock(t)
	a := tabDefaultApp(t, "")
	for _, step := range []struct {
		raw   string
		index int
	}{
		{"\x1b[9;5u", 1}, {"\x1b[9;5:2u", 2}, {"\x1b[9;5:3u", 2}, {"\x1b[9;5u", 0},
		{"\x1b[9;6u", 2}, {"\x1b[9;6:2u", 1}, {"\x1b[9;6:3u", 1}, {"\x02n", 2}, {"\x02p", 1},
	} {
		specFeed(a, step.raw)
		if got := a.st.ActiveWorkspace().ActiveTab; got != step.index {
			t.Fatalf("raw=%q tab=%d want=%d", step.raw, got, step.index)
		}
	}
}

func TestTabDefaultsReplacementDisableAndModalPriority(t *testing.T) {
	teststate.Lock(t)
	for _, body := range []string{"[keys]\nnext_tab='prefix+n'\nprevious_tab='prefix+p'", "[keys]\nnext_tab=''\nprevious_tab=''", "[keys]\nnext_tab=[]\nprevious_tab=[]"} {
		a := tabDefaultApp(t, body)
		specFeed(a, "\x1b[9;5u\x1b[9;6u")
		if a.st.ActiveWorkspace().ActiveTab != 0 {
			t.Fatal("overridden default remained", body)
		}
	}
	for _, mode := range []string{"dialog", "menu", "help", "copy"} {
		t.Run(mode, func(t *testing.T) {
			// An unrelated explicit key makes resolution materialize defaults; these
			// must still be distinguishable from explicit tab bindings after transport.
			a := tabDefaultApp(t, "[keys]\nhelp='prefix+?'\n")
			switch mode {
			case "dialog":
				a.newTabNameDialog()
			case "menu":
				a.openMainMenu(0, 0)
			case "help":
				a.openHelp()
			case "copy":
				a.cm = &copyMode{pane: a.focusedPane()}
			}
			specFeed(a, "\x1b[9;5u\x1b[9;6u")
			if a.st.ActiveWorkspace().ActiveTab != 0 {
				t.Fatal("default stole modal input")
			}
		})
	}
	a := tabDefaultApp(t, "[keys]\nnext_tab='ctrl+tab'\nprevious_tab='ctrl+shift+tab'\n")
	a.cm = &copyMode{pane: a.focusedPane()}
	specFeed(a, "\x1b[9;5u")
	if a.st.ActiveWorkspace().ActiveTab != 1 {
		t.Fatal("explicit binding lost existing copy precedence")
	}
}

func TestTabDefaultsDoNotCapturePlainTabAndHelpShowsBoth(t *testing.T) {
	teststate.Lock(t)
	a := tabDefaultApp(t, "")
	a.exec(model.CmdSplitFocused{Dir: layout.Vertical})
	before := a.focusedPane()
	for _, raw := range []string{"\t", "\x1b[9u", "\x1b[Z"} {
		specFeed(a, raw)
		if a.focusedPane() != before || a.st.ActiveWorkspace().ActiveTab != 0 {
			t.Fatal("plain tab captured", raw)
		}
	}
	specFeed(a, "\x02\t")
	if a.focusedPane() == before || a.st.ActiveWorkspace().ActiveTab != 0 {
		t.Fatal("prefix tab no longer cycles pane")
	}
	specFeed(a, "\x02\x1b[Z")
	if a.focusedPane() != before {
		t.Fatal("prefix shift tab no longer cycles backward")
	}
	for _, c := range []config.Config{{}, a.sidebarConfig} {
		a.sidebarConfig = c
		rows := a.helpRows()
		for _, name := range []string{"next tab", "previous tab"} {
			found := false
			for _, row := range rows {
				if row.Cmd == name {
					found = true
					if !strings.Contains(row.Bind, "prefix+") || !strings.Contains(row.Bind, "ctrl+") {
						t.Fatal("help omits default", row)
					}
				}
			}
			if !found {
				t.Fatal("missing help row", name)
			}
		}
	}
}
