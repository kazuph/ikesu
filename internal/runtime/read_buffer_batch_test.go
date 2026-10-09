package runtime

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/vt"
)

// Compare complete spans with byte-sized writes through real emulators. This
// catches metadata drift when batching meets wraps, styles, scrolling or chunks.
func TestRecordedShortLinesMatchFragmentedWrites(t *testing.T) {
	cases := map[string]string{
		"history_after_wrap":    "abcdefghXYZ\r\n" + strings.Repeat("short\r\n\r\n", ScrollbackSize+2) + "TAIL",
		"styled_wrap_and_links": "\x1b[31mabc\r\ndef\r\nabcdefghXYZ\r\n\x1b[0m\x1b]8;;https://example.com\x1b\\link\r\nmore\r\n\x1b]8;;\x1b\\END",
		"scroll_region":         "\x1b[2;4r\x1b[2;1H" + strings.Repeat("short\r\n", 12) + "\x1b[5;1Houtside\r\nend\r\n",
		"alternate_and_modes":   "main\r\nrow\r\n\x1b[?1049h" + strings.Repeat("alt\r\n", 12) + "\x1b[?1049l\x1b[?7lshort\r\nrow\r\n\x1b[?7hTAIL",
		"unicode_and_partial":   "界界\r\nabc\r\n1234567界Z\r\nshort\r\npartial",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			reference := recordedTestPane(t)
			for i := range len(text) {
				reference.writeRecorded([]byte(text[i : i+1]))
			}
			for _, size := range []int{2, 7, len(text)} {
				t.Run(fmt.Sprint(size), func(t *testing.T) {
					p := recordedTestPane(t)
					for i := 0; i < len(text); i += size {
						p.writeRecorded([]byte(text[i:min(i+size, len(text))]))
					}
					for _, source := range []string{"visible", "recent", "recent_unwrapped"} {
						for _, strip := range []bool{true, false} {
							want, _ := reference.ReadLines(source, strip)
							got, err := p.ReadLines(source, strip)
							if err != nil || !reflect.DeepEqual(got, want) {
								t.Fatalf("source=%s strip=%v: err=%v got=%q want=%q", source, strip, err, got, want)
							}
						}
					}
				})
			}
		})
	}
}

func recordedTestPane(t *testing.T) *Pane {
	t.Helper()
	p := &Pane{emu: vt.NewEmulator(8, 5)}
	p.emu.SetScrollbackSize(ScrollbackSize)
	p.emu.SetCallbacks(vt.Callbacks{AltScreen: func(on bool) { p.altScreen = on }})
	t.Cleanup(func() { _ = p.emu.Close() })
	return p
}
