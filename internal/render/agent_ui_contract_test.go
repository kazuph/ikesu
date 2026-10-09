package render

import (
	"reflect"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/layout"
	"github.com/kazuph/goterrace/internal/model"
)

func TestG9ToastByteWidthOriginAndWarning(t *testing.T) {
	for _, tc := range []struct {
		title, body string
		width       int
	}{
		{"title", "body", 11}, {"a", "a longer context", 22}, {"日本語", "本文", 15},
	} {
		toast := &Toast{Title: tc.title, Body: tc.body}
		view := layout.Rect{X: 7, Y: 9, W: 80, H: 25}
		r := ToastRect(toast, view, layout.Rect{})
		if r.W != tc.width || r.X+r.W != view.X+view.W || r.Y != view.Y {
			t.Fatalf("%+v: %+v", tc, r)
		}
		warning := layout.Rect{X: 40, Y: 10, W: 46, H: 2}
		r = ToastRect(toast, view, warning)
		if r.Y < warning.Y+warning.H {
			t.Fatal("warning overlaps", r)
		}
		for width := 0; width <= tc.width; width++ {
			view.W = width
			r = ToastRect(toast, view, layout.Rect{})
			if r.W > width || r.X < view.X || r.X+r.W > view.X+width {
				t.Fatal("view overflow", r, view)
			}
		}
	}
	s := model.New(t.TempDir(), 100, 30)
	out := Compose(Input{State: s, ConfigWarning: "configuration error", Toast: &Toast{Title: "notice", Body: "long context"}})
	if out.ToastRect.Y < out.ConfigWarningRect.Y+out.ConfigWarningRect.H {
		t.Fatal("composed overlap")
	}
	text := screenText(t, out, 100, 30)
	for _, v := range []string{"configuration error", "notice", "long context"} {
		if !strings.Contains(text, v) {
			t.Fatal("missing text", v, text)
		}
	}
}

func TestG5SeparatorExactTextAndColor(t *testing.T) {
	f := NewFrame(40, 15)
	m := &Menu{X: 2, Y: 2, Items: []MenuItem{{Label: "normal"}, {Separator: true}}, Sel: -1, Hover: -1}
	out := &Output{}
	drawMenu(f, m, out)
	row := out.MenuItemRows[1]
	var text strings.Builder
	for x := row.X; x < row.X+row.W; x++ {
		text.WriteString(f.Cell(x, row.Y).Content)
	}
	if strings.TrimRight(text.String(), " ") != "  --" {
		t.Fatal(text.String())
	}
	if !reflect.DeepEqual(f.Cell(row.X, row.Y).Style.Fg, f.Palette.Text) {
		t.Fatal("separator leading padding color")
	}
	for x := row.X + 1; x < row.X+4; x++ {
		if !reflect.DeepEqual(f.Cell(x, row.Y).Style.Fg, f.Palette.Dim) {
			t.Fatal("separator color")
		}
	}
	if reflect.DeepEqual(f.Cell(row.X+1, row.Y-1).Style.Fg, f.Cell(row.X+1, row.Y).Style.Fg) {
		t.Fatal("same color")
	}
}

func TestG9AgentRowActiveInactiveColors(t *testing.T) {
	for _, active := range []bool{false, true} {
		for _, state := range []string{"working", "idle", "blocked"} {
			f := NewFrame(100, 20)
			s := model.New(t.TempDir(), 100, 20)
			s.Workspaces = append(s.Workspaces, s.Workspaces[0])
			wi := 1
			if active {
				wi = 0
			}
			in := Input{Panes: map[int]*PaneSnap{1: {Agent: "codex", Status: StatusGlyph{Key: state, Rune: "!", Color: Yellow}, Tokens: map[string]string{"message": "metadata"}}}}
			drawConfiguredAgent(f, s, in, SidebarRow{Ws: wi, Pane: 1}, []config.SidebarToken{{Token: "agent"}, {Token: "$message"}}, 2, 5, 80)
			bg, fg := f.Palette.Surface, f.Palette.Subtle
			if active {
				bg, fg = f.Palette.SurfDim, f.Palette.Text
			}
			for x := 2; x < 82; x++ {
				c := f.Cell(x, 5)
				if !reflect.DeepEqual(c.Style.Bg, bg) || c.Style.Attrs&uv.AttrFaint != 0 {
					t.Fatalf("active=%v state=%s x=%d style=%+v", active, state, x, c.Style)
				}
				switch c.Content {
				case "!":
					if !reflect.DeepEqual(c.Style.Fg, Yellow) || c.Style.Attrs&uv.AttrBold == 0 {
						t.Fatal("status style", c.Style)
					}
				case "%":
					if !reflect.DeepEqual(c.Style.Fg, fg) || c.Style.Attrs&uv.AttrBold == 0 {
						t.Fatal("primary style", c.Style)
					}
				case "c", "m":
					if !reflect.DeepEqual(c.Style.Fg, fg) {
						t.Fatal("metadata style", c.Style)
					}
				}
			}
		}
	}
}
