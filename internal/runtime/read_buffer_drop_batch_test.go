package runtime

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestShortLineDropBatchMatchesByteWrites(t *testing.T) {
	rowBytes := int(unsafe.Sizeof(uv.Line{})) + 8*int(unsafe.Sizeof(uv.Cell{}))
	texts := []string{
		strings.Repeat("short\r\n\r\n", 30) + "TAIL",
		"abcdefghXYZ\r\n" + strings.Repeat("short\r\n", 30) + "TAIL",
		"\x1b[31mabc\r\ndef\r\n\x1b[0m" + strings.Repeat("row\r\n", 30) + "TAIL",
		"\x1b[2;4r\x1b[2;1H" + strings.Repeat("row\r\n", 30) + "TAIL",
		"main\r\n\x1b[?1049h" + strings.Repeat("alt\r\n", 30) + "\x1b[?1049l" + strings.Repeat("row\r\n", 30) + "TAIL",
		"界界\r\nabc\r\n1234567界Z\r\n" + strings.Repeat("row\r\n", 30) + "TAIL",
	}
	for _, budget := range []int{0, rowBytes, 3 * rowBytes, 10000} {
		for index, text := range texts {
			for _, size := range []int{2, 7, len(text)} {
				t.Run(fmt.Sprintf("budget%d/text%d/chunk%d", budget, index, size), func(t *testing.T) {
					want, got := recordedTestPane(t), recordedTestPane(t)
					for _, p := range []*Pane{want, got} {
						p.emu.Scrollback().SetMaxBytes(budget)
					}
					compare := func() {
						t.Helper()
						if got.emu.Render() != want.emu.Render() {
							t.Fatal("render differs")
						}
						for _, source := range []string{"visible", "recent", "recent_unwrapped"} {
							for _, strip := range []bool{true, false} {
								a, ae := got.ReadLines(source, strip)
								b, be := want.ReadLines(source, strip)
								if ae != nil || be != nil || !reflect.DeepEqual(a, b) {
									t.Fatalf("%s strip%v got%q want%q errors%v/%v", source, strip, a, b, ae, be)
								}
							}
						}
						if got.read.historyLen() != got.emu.ScrollbackLen() {
							t.Fatal("history length differs")
						}
					}
					for i := 0; i < len(text); i += size {
						end := min(i+size, len(text))
						for j := i; j < end; j++ {
							want.writeRecorded([]byte(text[j : j+1]))
						}
						got.writeRecorded([]byte(text[i:end]))
						compare()
					}
					// A later configuration change still needs the drop callback.
					for _, p := range []*Pane{want, got} {
						p.emu.Scrollback().SetMaxBytes(0)
					}
					compare()
					if got.read.historyLen() != 0 {
						t.Fatal("drop callback was not restored")
					}
				})
			}
		}
	}
}
