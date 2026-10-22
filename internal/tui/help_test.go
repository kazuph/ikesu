package tui

import (
	"github.com/kazuph/ikesu/internal/teststate"
	"strings"

	"github.com/kazuph/ikesu/internal/model"
	"testing"
)

// SPEC G6: the keybind help lists every command ikesu implements, ordered
// global → navigation → workspaces/tabs → panes, ending with cycle pane next →
// previous. Unconfigured command bindings are shown as unset.
func TestHelpRowsContract(t *testing.T) {
	a := &App{}
	rows := a.helpRows()
	if len(rows) == 0 {
		t.Fatal("empty help")
	}
	// Group order.
	groups := []string{}
	for _, r := range rows {
		if r.Group {
			groups = append(groups, r.Cmd)
		}
	}
	want := []string{"global", "navigation", "workspaces / tabs", "panes"}
	if strings.Join(groups, "|") != strings.Join(want, "|") {
		t.Fatalf("groups = %v", groups)
	}
	// Required entries.
	cmds := map[string]bool{}
	var last []string
	for _, r := range rows {
		if !r.Group {
			cmds[r.Cmd] = true
			last = append(last, r.Cmd)
		}
	}
	for _, cmd := range []string{
		"copy mode", "split vertical", "split horizontal",
		"toggle sidebar", "cycle pane next", "cycle pane previous",
	} {
		if !cmds[cmd] {
			t.Fatalf("missing %q in help", cmd)
		}
	}
	// Final entries: cycle next → cycle previous.
	if n := len(last); n < 6 || last[n-6] != "cycle pane next" || last[n-5] != "cycle pane previous" || last[n-4] != "focus history back" || last[n-3] != "focus history forward" || last[n-2] != "last pane" || last[n-1] != "" {
		t.Fatalf("last entries = %v", last[max(0, n-3):])
	}
	for i, r := range last {
		if r == "rename pane" && (last[i+1] != "edit scrollback" || last[i+2] != "copy mode") {
			t.Fatal("pane command order", last)
		}
	}
}

// Configured bindings render sorted and joined; nil uses defaults.
func TestBindKeys(t *testing.T) {
	got := bindKeys(map[rune]bool{'v': true, '%': true})
	if got != "prefix+% / prefix+v" {
		t.Fatalf("bindKeys = %q", got)
	}
	if got := bindKeys(nil, '[', ']'); got != "prefix+[ / prefix+]" {
		t.Fatalf("fallback = %q", got)
	}
	if got := bindKeys(map[rune]bool{'-': true}); got != "prefix+-" {
		t.Fatalf("minus name = %q", got)
	}
}

// The help overlay scroll clamps and closes on esc/enter/q.
func TestHelpScrollClamp(t *testing.T) {
	a := &App{st: model.New("", 80, 24)}
	a.openHelp()
	for i := 0; i < 100; i++ {
		a.helpScrollBy(1)
	}
	if a.help.scroll != a.helpMaxScroll() {
		t.Fatalf("scroll %d > max %d", a.help.scroll, a.helpMaxScroll())
	}
	for i := 0; i < 100; i++ {
		a.helpScrollBy(-1)
	}
	if a.help.scroll != 0 {
		t.Fatalf("scroll %d < 0", a.help.scroll)
	}
}

func TestKbdHostSeqs(t *testing.T) {
	teststate.Lock(t)
	for _, tc := range []struct{ host, tmux, pane, extra string }{
		{"", "", "", ""}, {"ghostty", "", "", ""}, {"Ghostty", "", "", ""},
		{"iTerm.app", "", "", ""}, {"WezTerm", "", "", "\x1b[>4;1m"},
		{"", "", "0", "\x1b[>4;1m"}, {"", "private-tmux", "", "\x1b[>4;2m"},
		{"WezTerm", "private-tmux", "0", "\x1b[>4;2m"},
	} {
		teststate.Setenv(t, "TERM_PROGRAM", tc.host)
		teststate.Setenv(t, "TMUX", tc.tmux)
		teststate.Setenv(t, "WEZTERM_PANE", tc.pane)
		leave := "\x1b[<1u"
		if tc.extra != "" {
			leave = "\x1b[>4;0m" + leave
		}
		if KbdEnterSeq() != "\x1b[>7u"+tc.extra || KbdLeaveSeq() != leave {
			t.Fatalf("host=%q tmux=%q pane=%q: %q / %q", tc.host, tc.tmux, tc.pane, KbdEnterSeq(), KbdLeaveSeq())
		}
	}
}
