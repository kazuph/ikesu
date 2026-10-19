package render

import (
	"fmt"
	"strings"
	"testing"
)

func TestClipboardFeedbackSidebarBothRuntimes(t *testing.T) {
	for _, server := range []bool{false, true} {
		for _, visible := range []bool{false, true} {
			for _, enabled := range []bool{false, true} {
				for _, width := range []int{8, 26} {
					t.Run(fmt.Sprint(server, visible, enabled, width), func(t *testing.T) {
						s := testState(100, 30)
						s.SidebarVisible, s.SidebarSize, s.SidebarManual = visible, width, true
						s.SidebarMin = 1
						in := Input{State: s, ServerUI: server, Copied: "Copied 3 lines"}
						in.SidebarConfig.UI.Toast.Clipboard.Enabled = &enabled
						f := Compose(in).Frame
						area := s.SidebarArea()
						var row strings.Builder
						for x := area.X; x < area.X+area.W; x++ {
							row.WriteString(f.Cell(x, area.Y+area.H-1).Content)
						}
						got := row.String()
						if visible && enabled {
							want := truncate(in.Copied, area.W-2)
							if !strings.Contains(got, want) {
								t.Fatalf("row=%q want=%q", got, want)
							}
							if width == 8 && !strings.Contains(got, "…") {
								t.Fatal("missing ellipsis", got)
							}
						} else if strings.Contains(got, "Cop") {
							t.Fatal("feedback must be hidden", got)
						}
					})
				}
			}
		}
	}
}

func TestClipboardFeedbackHiddenSuppressesAllSurfaces(t *testing.T) {
	for _, server := range []bool{false, true} {
		for _, hidden := range []bool{false, true} {
			s := testState(100, 30)
			s.SidebarVisible = !hidden
			enabled := hidden
			in := Input{State: s, ServerUI: server, Copied: "Copied 3 lines"}
			in.SidebarConfig.UI.Toast.Clipboard.Enabled = &enabled
			out := Compose(in)
			text := screenText(t, out, s.Cols, s.Rows)
			if strings.Contains(text, "Copied") {
				t.Fatal("suppressed feedback visible", server, hidden, text)
			}
		}
	}
}
