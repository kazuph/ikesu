package render

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestStage5WideTitleKeepsEllipsisWithinCells(t *testing.T) {
	for _, text := range []string{"%2 日本語の長いペインタイトルを表示して省略記号を確認する", "%2 ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"} {
		got := truncateEllipsis(text, 28)
		if ansi.StringWidth(got) > 28 || !strings.HasSuffix(got, "…") {
			t.Fatalf("title=%q width=%d", got, ansi.StringWidth(got))
		}
	}
}

func TestStage5CopyPartialWidthAndHit(t *testing.T) {
	for width := 1; width <= 5; width++ {
		s := testState(26+width, 30)
		out := Compose(Input{State: s, ServerUI: true})
		hit := out.ActionBar.Copy
		if hit.X != 26 || hit.Y != 29 || hit.W != width || hit.H != 1 {
			t.Fatalf("width=%d hit=%+v", width, hit)
		}
		for x := 0; x < width; x++ {
			if out.Frame.Cell(26+x, 29).Content != string(" COPY "[x]) {
				t.Fatalf("width=%d x=%d", width, x)
			}
		}
	}
}
