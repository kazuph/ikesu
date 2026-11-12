package tui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/teststate"
)

// An attached screen (viewer) drags a selection above a popup: the popup's
// history must scroll on the viewer's ticks, as on the server's own screen
// (2026-11-12: dragging above a popup never reached the hidden lines).
func TestViewerPopupDragAboveScrollsHistory(t *testing.T) {
	teststate.Lock(t)
	teststate.Setenv(t, "SHELL", "/bin/sh")
	a := specRuntimeApp(t)
	t.Cleanup(a.sup.KillAll)
	var out bytes.Buffer
	v := a.NewViewer(&out, a.st.Cols, a.st.Rows, nil)
	width, height := api.PopupSize{Cells: 40}, api.PopupSize{Cells: 12}
	command := "i=0; while [ $i -lt 100 ]; do printf 'line %s\\r\\n' $i; i=$((i+1)); done; printf READY; exec sleep 60"
	if _, err := a.OpenPopup(a.cwd, command, &width, &height); err != nil {
		t.Fatal(err)
	}
	id := a.st.Popup.PaneID
	specWaitText(t, a.sup.Get(id), "READY")
	var inner = a.innerRect(id)
	v.with(a, func(ui *App) { inner = ui.innerRect(id) })
	sgr := func(button, x, y int, final byte) []byte {
		return []byte(fmt.Sprintf("\x1b[<%d;%d;%d%c", button, x+1, y+1, final))
	}
	// Press inside, drag above the popup's top edge, then only ticks.
	v.Input(a, sgr(0, inner.X+2, inner.Y+5, 'M'))
	v.Input(a, sgr(32, inner.X+2, inner.Y+1, 'M'))
	v.Input(a, sgr(32, inner.X+2, inner.Y-1, 'M'))
	if v.app.popupAutoScroll != 1 {
		t.Fatalf("drag above the popup did not start auto-scroll: %d", v.app.popupAutoScroll)
	}
	for range 5 {
		v.Tick(a)
	}
	if got := v.app.scroll[id]; got < 15 {
		t.Fatalf("popup history scrolled %d lines on the viewer's ticks, want at least 15", got)
	}
	out.Reset()
	v.Input(a, sgr(0, inner.X+2, inner.Y-1, 'm'))
	if v.app.popupAutoScroll != 0 {
		t.Fatal("auto-scroll kept running after release")
	}
	// The copy (OSC 52) reaches lines that were above the popup's view.
	m := regexp.MustCompile("\x1b]52;c;([A-Za-z0-9+/=]*)").FindStringSubmatch(out.String())
	if m == nil {
		t.Fatal("selection was not copied")
	}
	copied, _ := base64.StdEncoding.DecodeString(m[1])
	if !strings.Contains(string(copied), "line 80") {
		t.Fatalf("copy did not reach the scrolled-in lines: %q", copied)
	}
}
