package runtime

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"strings"
	"testing"
	"unsafe"
)

func TestByteBoundedScrollbackKeepsLogicalRowsStylesAndResize(t *testing.T) {
	p := recordedTestPane(t)
	rowBytes := int(unsafe.Sizeof(uv.Line{})) + 8*int(unsafe.Sizeof(uv.Cell{}))
	p.emu.Scrollback().SetMaxBytes(3 * rowBytes)
	p.writeRecorded([]byte(strings.Repeat("\x1b[31mabcdefghXYZ\x1b[0m\r\n", 30) + "TAIL"))
	if p.emu.Scrollback().Bytes() > 3*rowBytes || p.read.historyLen() != p.emu.ScrollbackLen() {
		t.Fatal(p.emu.Scrollback().Bytes(), p.read.historyLen(), p.emu.ScrollbackLen())
	}
	rows, err := p.ReadLines("recent_unwrapped", true)
	if err != nil || !strings.Contains(strings.Join(rows, "\n"), "abcdefghXYZ") {
		t.Fatal(rows, err)
	}
	p.resizeTerminal(5, 5)
	if p.read.historyLen() != p.emu.ScrollbackLen() || p.emu.Scrollback().Bytes() > 3*rowBytes {
		t.Fatal("resize budget or metadata drift")
	}
	p.emu.Scrollback().SetMaxBytes(0)
	p.writeRecorded([]byte(strings.Repeat("z\r\n", 10)))
	if p.emu.ScrollbackLen() != 0 || p.read.historyLen() != 0 {
		t.Fatal("zero budget", p.emu.ScrollbackLen(), p.read.historyLen())
	}
}
func TestByteBoundedScrollbackHasNoThousandRowCeiling(t *testing.T) {
	p := &Pane{emu: vt.NewEmulator(80, 5)}
	defer p.emu.Close()
	p.emu.Scrollback().SetMaxBytes(10000000)
	p.writeRecorded([]byte(strings.Repeat("x\r\n", 2500)))
	if p.emu.ScrollbackLen() <= ScrollbackSize || p.read.historyLen() != p.emu.ScrollbackLen() {
		t.Fatal(p.emu.ScrollbackLen(), p.read.historyLen())
	}
}
