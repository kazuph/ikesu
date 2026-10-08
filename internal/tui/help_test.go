package tui

import (
	"strings"

	"github.com/kazuph/goterrace/internal/model"
	"testing"
)

// SPEC G6: the keybind help lists every command gtx implements, ordered
// global → workspaces/tabs → panes, ending with cycle pane next →
// previous. Commands gtx does not implement are absent.
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
	want := []string{"global", "workspaces / tabs", "panes"}
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
	// Unimplemented commands stay absent.
	for _, cmd := range []string{"zoom pane"} {
		if cmds[cmd] {
			t.Fatalf("unimplemented %q listed in help", cmd)
		}
	}
	// Final entries: cycle next → cycle previous.
	if n := len(last); n < 4 || last[n-4] != "cycle pane next" || last[n-3] != "cycle pane previous" || last[n-2] != "focus history back" || last[n-1] != "focus history forward" {
		t.Fatalf("last entries = %v", last[max(0, n-3):])
	}
	for i, r := range last {
		if r == "rename pane" && (last[i+1] != "copy mode" || last[i+2] != "edit scrollback") {
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
	if got := bindKeys(map[rune]bool{'-': true}); got != "prefix+minus" {
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

// Ghostty hosts get modifyOtherKeys on/off sequences; others get none.
func TestKbdHostSeqs(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "ghostty")
	if KbdEnterSeq() != "\x1b[>4;1m" || KbdLeaveSeq() != "\x1b[>4;0m" {
		t.Fatalf("ghostty seqs = %q / %q", KbdEnterSeq(), KbdLeaveSeq())
	}
	t.Setenv("TERM_PROGRAM", "Ghostty")
	if KbdEnterSeq() == "" {
		t.Fatal("Ghostty (capital) must also match")
	}
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	if KbdEnterSeq() != "" || KbdLeaveSeq() != "" {
		t.Fatal("non-ghostty host must get no modifyOtherKeys")
	}
}
