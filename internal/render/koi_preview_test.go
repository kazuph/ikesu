package render

// SPIKE: KOI_PREVIEW=<dir> writes real Compose() frames as an animated HTML
// preview so the koi prototype can be screenshotted without a terminal.

import (
	"fmt"
	"html"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
)

func TestKoiCountCapsDensity(t *testing.T) {
	for _, tc := range []struct{ agents, w, h, want int }{
		{0, 30, 10, 0}, {1, 30, 10, 1}, {4, 30, 10, 4}, {9, 30, 10, 4},
		{40, 30, 20, 8}, {40, 30, 10, 4}, {3, 30, 2, 0},
	} {
		if got := KoiCount(tc.agents, tc.w, tc.h); got != tc.want {
			t.Fatalf("KoiCount(%d,%d,%d)=%d want %d", tc.agents, tc.w, tc.h, got, tc.want)
		}
	}
}

func cssColor(c color.Color, def string) string {
	if c == nil {
		return def
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

func frameHTML(f *Frame) string {
	var sb strings.Builder
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			c := f.Cell(x, y)
			if c == nil || c.Width == 0 {
				continue
			}
			content := c.Content
			if content == "" {
				content = " "
			}
			fmt.Fprintf(&sb, `<span style="color:%s;background:%s">%s</span>`,
				cssColor(c.Style.Fg, "#d8d8d8"), cssColor(c.Style.Bg, "#16161e"), html.EscapeString(content))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func TestKoiPreview(t *testing.T) {
	dir := os.Getenv("KOI_PREVIEW")
	if dir == "" {
		t.Skip("set KOI_PREVIEW=<dir>")
	}
	for _, agents := range []int{3, 30} {
		s := model.New("/tmp", 150, 40)
		panes := map[int]*PaneSnap{}
		for i := 0; i < 4; i++ {
			s.Apply(model.CmdNewWorkspace{Cwd: fmt.Sprintf("/tmp/w%d", i)})
		}
		ids := []int{}
		for _, ws := range s.Workspaces {
			ids = append(ids, ws.TabRef().Layout.PaneIDs()...)
		}
		for i := 0; i < agents; i++ {
			pid := ids[i%len(ids)]
			if i >= len(ids) {
				s.Apply(model.CmdSplitFocused{Dir: layout.Vertical})
				pid = s.FocusedPane()
			}
			panes[pid] = &PaneSnap{Agent: []string{"claude", "codex"}[i%2], Status: StatusGlyph{Key: "working", Rune: "●", Color: Green}}
		}
		var frames []string
		for t0 := 0; t0 < 160; t0++ {
			in := Input{State: s, Panes: panes, Spin: t0, Koi: true}
			frames = append(frames, frameHTML(Compose(in).Frame))
		}
		var sb strings.Builder
		sb.WriteString(`<!doctype html><meta charset=utf-8><style>body{background:#16161e;margin:0}pre{font:13px/1.0 Menlo,monospace;margin:8px;letter-spacing:0}</style><pre id=p></pre><script>const F=[`)
		for i, fr := range frames {
			if i > 0 {
				sb.WriteString(",")
			}
			fmt.Fprintf(&sb, "%q", fr)
		}
		sb.WriteString(`];let i=0;const p=document.getElementById('p');function s(){p.innerHTML=F[i%F.length];i++}s();const q=new URLSearchParams(location.search);if(q.has('f')){i=+q.get('f');s()}else setInterval(s,128);</script>`)
		out := filepath.Join(dir, fmt.Sprintf("koi-%dagents.html", agents))
		if err := os.WriteFile(out, []byte(sb.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
