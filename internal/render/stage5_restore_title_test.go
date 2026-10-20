package render

import (
	"github.com/kazuph/ikesu/internal/model"
	"strings"
	"testing"
)

// Expected cells were observed on isolated herdr 0.2.57 with mobile mode disabled.
func TestStage5RestoreTitleObservedRows(t *testing.T) {
	cases := []struct {
		title, restore string
		width          int
		want           string
	}{
		{"ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ", "saved session", 34, "┌ %1 ABCDEFGHIJKLM saved session ┐"},
		{"ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ", "saved session", 40, "┌ %1 ABCDEFGHIJKLMNOPQRS saved session ┐"},
		{"ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ", "saved session", 50, "┌ %1 ABCDEFGHIJKLMNOPQRSTUVWXYZ012 saved session ┐"},
		{"ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ", "no saved session", 34, "┌ %1 ABCDEFGHIJ no saved session ┐"},
		{"ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ", "no saved session", 40, "┌ %1 ABCDEFGHIJKLMNOP no saved session ┐"},
		{"ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ", "no saved session", 50, "┌ %1 ABCDEFGHIJKLMNOPQRSTUVWXYZ no saved session ┐"},
		{"日本語の長いペインタイトルを表示して省略記号を確認する", "saved session", 34, "┌ %1 日本語の長いペsaved session ┐"},
		{"日本語の長いペインタイトルを表示して省略記号を確認する", "saved session", 40, "┌ %1 日本語の長いペインタsaved session ┐"},
		{"日本語の長いペインタイトルを表示して省略記号を確認する", "saved session", 50, "┌ %1 日本語の長いペインタイトルを表saved session ┐"},
		{"日本語の長いペインタイトルを表示して省略記号を確認する", "no saved session", 34, "┌ %1 日本語の長 no saved session ┐"},
		{"日本語の長いペインタイトルを表示して省略記号を確認する", "no saved session", 40, "┌ %1 日本語の長いペイ no saved session ┐"},
		{"日本語の長いペインタイトルを表示して省略記号を確認する", "no saved session", 50, "┌ %1 日本語の長いペインタイトル no saved session ┐"},
	}
	for _, c := range cases {
		st := testState(model.SidebarWidth+c.width, 30)
		st.RightSidebarMode = "hidden" // Keep the original pane geometry for this content regression.
		id := st.ActiveWorkspace().TabRef().Layout.Focused()
		out := Compose(Input{State: st, ServerUI: true, Panes: map[int]*PaneSnap{id: {Label: c.title, Agent: "codex", Restore: c.restore}}})
		var row strings.Builder
		for x := model.SidebarWidth; x < st.Cols; x++ {
			row.WriteString(out.Frame.Cell(x, 1).Content)
		}
		if row.String() != c.want {
			t.Errorf("width=%d restore=%s title=%s\ngot %q\nwant %q", c.width, c.restore, c.title, row.String(), c.want)
		}
	}
}
