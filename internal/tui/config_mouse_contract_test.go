package tui

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestConfigCopyOnSelectChangesRealMouseSelection(t *testing.T) {
	teststate.Lock(t)
	for _, on := range []bool{false, true} {
		a := specRuntimeApp(t)
		p, err := a.sup.SpawnWith(a.focusedPane(), a.cwd, 80, 20, nil, []string{"/bin/sh", "-c", "printf READY; exec /bin/cat"})
		if err != nil {
			t.Fatal(err)
		}
		specWaitText(t, p, "READY")
		c, err := config.Decode([]byte(fmt.Sprintf("[ui]\ncopy_on_select=%t\n", on)))
		if err != nil {
			t.Fatal(err)
		}
		a.SetSidebarConfig(c, false)
		inner, _ := model.PaneContentRect(a.st.DisplayPaneRects()[0].Rect, true)
		specFeed(a, fmt.Sprintf("\x1b[<0;%d;%dM", inner.X+2, inner.Y+1))
		if (a.sel != nil) != on {
			t.Fatal(on, a.sel)
		}
		// Do not release the selection: no test writes the user's clipboard.
	}
}

func TestConfigRightClickModifierChangesRealMouseRoute(t *testing.T) {
	teststate.Lock(t)
	for _, modifier := range []string{"off", "ctrl"} {
		a := specRuntimeApp(t)
		p, err := a.sup.SpawnWith(a.focusedPane(), a.cwd, 80, 20, nil, []string{"/bin/sh", "-c", "printf '\033[?1000hREADY'; exec /bin/cat"})
		if err != nil {
			t.Fatal(err)
		}
		specWaitText(t, p, "READY")
		c, err := config.Decode([]byte("[ui]\nright_click_passthrough_modifier='" + modifier + "'\n"))
		if err != nil {
			t.Fatal(err)
		}
		a.SetSidebarConfig(c, false)
		inner, _ := model.PaneContentRect(a.st.DisplayPaneRects()[0].Rect, true)
		specFeed(a, fmt.Sprintf("\x1b[<18;%d;%dM", inner.X+2, inner.Y+1))
		if (a.menu != nil) != (modifier == "off") || (a.fwd == a.focusedPane()) != (modifier == "ctrl") {
			t.Fatal(modifier, a.menu, a.fwd)
		}
	}
}

func TestConfigMouseScrollLinesChangesRealWheelDistance(t *testing.T) {
	teststate.Lock(t)
	for _, lines := range []int{2, 5} {
		a := specRuntimeApp(t)
		p, err := a.sup.SpawnWith(a.focusedPane(), a.cwd, 80, 20, nil, []string{"/bin/sh", "-c", "i=0; while [ $i -lt 80 ]; do echo line-$i; i=$((i+1)); done; printf READY; exec /bin/cat"})
		if err != nil {
			t.Fatal(err)
		}
		specWaitText(t, p, "READY")
		c, err := config.Decode([]byte(fmt.Sprintf("[ui]\nmouse_scroll_lines=%d\n", lines)))
		if err != nil {
			t.Fatal(err)
		}
		a.SetSidebarConfig(c, false)
		inner, _ := model.PaneContentRect(a.st.DisplayPaneRects()[0].Rect, true)
		specFeed(a, fmt.Sprintf("\x1b[<64;%d;%dM", inner.X+2, inner.Y+1))
		if a.scroll[a.focusedPane()] != lines {
			t.Fatal(lines, a.scroll)
		}
	}
}

func TestConfigDrawnHostCursorChangesEncodedRenderedCells(t *testing.T) {
	teststate.Lock(t)
	counts := map[string]int{}
	for _, mode := range []string{"native", "drawn"} {
		a := specRuntimeApp(t)
		p, err := a.sup.SpawnWith(a.focusedPane(), a.cwd, 80, 20, nil, []string{"/bin/sh", "-c", "printf READY; exec /bin/cat"})
		if err != nil {
			t.Fatal(err)
		}
		specWaitText(t, p, "READY")
		c, err := config.Decode([]byte("[ui]\nhost_cursor='" + mode + "'\n"))
		if err != nil {
			t.Fatal(err)
		}
		a.SetSidebarConfig(c, false)
		var out bytes.Buffer
		a.out = &out
		a.draw()
		emu := vt.NewEmulator(a.st.Cols, a.st.Rows)
		go func() { _, _ = io.Copy(io.Discard, emu) }()
		_, _ = emu.Write(out.Bytes())
		inner, _ := model.PaneContentRect(a.st.DisplayPaneRects()[0].Rect, true)
		for y := inner.Y; y < inner.Y+inner.H; y++ {
			for x := inner.X; x < inner.X+inner.W; x++ {
				if emu.CellAt(x, y).Style.Attrs&uv.AttrReverse != 0 {
					counts[mode]++
				}
			}
		}
		emu.Close()
	}
	if counts["drawn"] != counts["native"]+1 {
		t.Fatal(counts)
	}
}
