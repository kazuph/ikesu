package render

import (
	"reflect"
	"strings"
	"testing"
)

func TestParityHelpWordWrapObservedCommandBoundary(t *testing.T) {
	// help-size and help-compact captures retain padding across a wrap and a whole word at the right edge.
	for _, tc := range []struct {
		width int
		want  []string
	}{
		{23, []string{" prefix+o", "      open notification", "target"}},
		{53, []string{" prefix+o" + strings.Repeat(" ", 21) + "open notification", "target"}},
	} {
		lines := HelpLines([]HelpRow{{Bind: "prefix+o", Cmd: "open notification target"}}, tc.width, Palette{})
		var got []string
		for _, line := range lines {
			var text strings.Builder
			for _, cell := range line {
				text.WriteString(cell.Content)
			}
			got = append(got, text.String())
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("width %d: %q want %q", tc.width, got, tc.want)
		}
	}
}
