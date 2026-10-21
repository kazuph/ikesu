package render

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/kazuph/ikesu/internal/model"
)

func titleInput(states ...string) Input {
	s := model.New("/tmp", 160, 45)
	in := Input{State: s, Panes: map[int]*PaneSnap{}}
	for i, state := range states {
		if i > 0 {
			s.Apply(model.CmdNewWorkspace{Cwd: "/tmp"})
		}
		in.Panes[s.FocusedPane()] = &PaneSnap{Status: StatusGlyph{Key: state}}
	}
	return in
}

func TestWindowTitleSummaryCombinationsAndMachines(t *testing.T) {
	for mask := 0; mask < 8; mask++ {
		t.Run(fmt.Sprint(mask), func(t *testing.T) {
			states, marks := []string{"idle", "unknown"}, ""
			for i, key := range []string{"blocked", "working", "done"} {
				if mask&(1<<i) != 0 {
					states = append(states, key, key)
					marks += []string{"🔴", "🟡", "🔵"}[i]
				}
			}
			in := titleInput(states...)
			want := "ikesu"
			if marks != "" {
				want = marks + " " + want
			}
			if got := WindowTitle(in, "default", nil); got != want {
				t.Fatal(got, want)
			}
		})
	}
	local, remote := titleInput("done", "working"), titleInput("blocked")
	groups := []MachineGroup{{ID: "local", Input: &local}, {ID: "remote", Input: &remote}, {ID: "offline"}}
	if got := WindowTitle(local, "coding", groups); got != "🔴🟡🔵 ikesu:coding" {
		t.Fatal(got)
	}
	local.SidebarConfig.UI.WindowTitle = "plain"
	if got := WindowTitle(local, "coding", groups); got != "ikesu:coding" {
		t.Fatal(got)
	}
}

func TestWindowTitleUsesWorkspacePriorityAcrossTabs(t *testing.T) {
	in := titleInput("working")
	for _, key := range []string{"done", "blocked", "idle"} {
		in.State.Apply(model.CmdNewTab{})
		in.Panes[in.State.FocusedPane()] = &PaneSnap{Status: StatusGlyph{Key: key}}
	}
	if got := WindowTitle(in, "", nil); got != "🔴 ikesu" {
		t.Fatal("lower priority panes must not add workspace states", got)
	}
}

func TestWindowTitleOnlyWritesChangedBytesAfterActivation(t *testing.T) {
	in := titleInput("working")
	var title WindowTitleState
	var out bytes.Buffer
	paint := func(want string) {
		t.Helper()
		out.Reset()
		if err := title.Painted(&out, in, "", nil); err != nil || out.String() != want {
			t.Fatalf("bytes=%q want=%q error=%v", out.String(), want, err)
		}
	}
	paint("\x1b]0;🟡 ikesu\x07")
	paint("\x1b]0;🟡 ikesu\x07")
	paint("")
	in.Spin++
	paint("")
	for _, state := range []struct{ key, title string }{{"blocked", "🔴 ikesu"}, {"done", "🔵 ikesu"}, {"idle", "ikesu"}} {
		in.Panes[in.State.FocusedPane()].Status.Key = state.key
		paint("\x1b]0;" + state.title + "\x07")
		paint("")
	}
}
