package runtime

import (
	"reflect"
	"strings"
	"testing"
	"unsafe"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestIndexedHistoryOutputAndHandoffMetadataStayAligned(t *testing.T) {
	rowBytes := int(unsafe.Sizeof(uv.Line{})) + 8*int(unsafe.Sizeof(uv.Cell{}))
	for _, budget := range []int{-1, 0, 3 * rowBytes, 30 * rowBytes} {
		p, restored := recordedTestPane(t), recordedTestPane(t)
		if budget >= 0 {
			p.emu.Scrollback().SetMaxBytes(budget)
		}
		p.writeRecorded([]byte(strings.Repeat("\x1b[31mabcdefghXYZ\x1b[0m\r\n", 30) + "TAIL"))
		p.resizeTerminal(7, 5)
		data, err := p.emu.MarshalHandoff()
		if err != nil {
			t.Fatal(err)
		}
		if err := restored.emu.UnmarshalHandoff(data); err != nil {
			t.Fatal(err)
		}
		restored.read = p.read.transfer().restore()
		if restored.read.byteHistory {
			restored.emu.Scrollback().OnDrop = restored.read.dropHistory
		}
		compare := func() {
			t.Helper()
			for _, source := range []string{"visible", "recent", "recent_unwrapped"} {
				for _, strip := range []bool{false, true} {
					a, ae := p.ReadLines(source, strip)
					b, be := restored.ReadLines(source, strip)
					if ae != nil || be != nil || !reflect.DeepEqual(a, b) {
						t.Fatalf("budget=%d source=%s got=%q restored=%q errors=%v/%v", budget, source, a, b, ae, be)
					}
				}
			}
			if p.read.historyLen() != p.emu.ScrollbackLen() || restored.read.historyLen() != restored.emu.ScrollbackLen() || p.emu.Scrollback().Bytes() != restored.emu.Scrollback().Bytes() {
				t.Fatal("history metadata or byte charge drifted")
			}
		}
		compare()
		for _, width := range []int{9, 5, 8} {
			for _, pane := range []*Pane{p, restored} {
				pane.writeRecorded([]byte(strings.Repeat("new\r\n", 30) + "\x1b]8;;https://example.com\x1b\\界e\u0301\x1b]8;;\x1b\\"))
				pane.resizeTerminal(width, 5)
			}
			compare()
		}
	}
}
