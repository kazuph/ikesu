package render

import (
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/model"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDecisionDialogFieldsWrapAndBounds(t *testing.T) {
	expires := int64(120000)
	body := strings.Repeat("本文 ", 50)
	d := DecisionDialog{Decision: api.Decision{Title: "Proceed?", Body: &body, Origin: &api.DecisionOrigin{PaneID: "p1", Agent: "codex", Cwd: "/tmp", Branch: "main", Command: strings.Repeat("cmd ", 50), Reason: "reason", Machine: "origin"}, ExpiresUnixMS: &expires, AllowText: true, Options: []api.DecisionOption{{ID: "yes", Label: "Approve", Role: "approve"}, {ID: "no", Label: "Reject", Role: "reject"}}}, Machine: "Mini", Index: 0, Total: 2}
	for _, size := range [][2]int{{80, 24}, {160, 45}, {40, 16}} {
		out := Compose(Input{State: model.New("/tmp", size[0], size[1])})
		r := ComposeDecision(out, &d, 2, time.UnixMilli(0))
		if r.Outer.X < 0 || r.Outer.Y < 0 || r.Outer.X+r.Outer.W > size[0] || r.Outer.Y+r.Outer.H > size[1] || len(r.Options) != 2 || r.Text.W == 0 {
			t.Fatal(size, r)
		}
		lines := decisionLines(&d, r.Outer.W-2, time.UnixMilli(0))
		joined := strings.Join(lines, "\n")
		for _, want := range []string{"Proceed?", "Mini · 1/2", "expires in 120s", "pane: p1", "agent: codex", "cwd: /tmp", "branch: main", "reason: reason", "machine: origin"} {
			if !strings.Contains(joined, want) {
				t.Fatal(want, joined)
			}
		}
		for _, line := range lines {
			if len([]rune(line)) > r.Outer.W-2 {
				t.Fatal("unwrapped line", line)
			}
		}
		d.Scroll = r.ScrollRows
		after := ComposeDecision(out, &d, 2, time.UnixMilli(0))
		if after.VisibleRows == 0 {
			t.Fatal(after)
		}
	}
}

func TestDecisionFocusedSelectionMatchesExistingMenu(t *testing.T) {
	menu := Compose(Input{State: model.New("/tmp", 80, 24), Menu: &Menu{X: 4, Y: 4, Sel: 0, Hover: -1, Items: []MenuItem{{Label: "Approve"}, {Label: "Reject"}}}})
	mr := menu.MenuItemRows[0]
	want := menu.Frame.Cell(mr.X+1, mr.Y).Style
	d := DecisionDialog{Decision: api.Decision{Title: "Choose", Options: []api.DecisionOption{{ID: "yes", Label: "Approve"}, {ID: "no", Label: "Reject"}}}, Machine: "Local", Total: 1}
	for _, focused := range []bool{false, true} {
		for selected := 0; selected < 2; selected++ {
			d.Focused, d.Selected = focused, selected
			out := Compose(Input{State: model.New("/tmp", 80, 24)})
			regions := ComposeDecision(out, &d, 1, time.UnixMilli(0))
			for i, r := range regions.Options {
				for x := r.X; x < r.X+r.W; x++ {
					style := out.Frame.Cell(x, r.Y).Style
					equal := reflect.DeepEqual(style, want)
					if equal != (focused && i == selected) {
						t.Fatalf("focused=%v selected=%d row=%d x=%d: %+v", focused, selected, i, x, style)
					}
				}
			}
		}
	}
}
