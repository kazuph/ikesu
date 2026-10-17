package tui

import (
	"fmt"
	"path/filepath"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestStage3RightClickStripsModifierThroughoutRealPTYGesture(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.sidebarConfig, _ = config.Decode([]byte("[ui]\nright_click_passthrough_modifier='ctrl'\n"))
	pid := a.focusedPane()
	inner := a.innerRect(pid)
	path := filepath.Join(t.TempDir(), "mouse")
	want := "\x1b[<2;3;3M\x1b[<34;3;3M\x1b[<2;3;3m\x1b[<16;3;3M\x1b[<16;3;3m"
	command := fmt.Sprintf("stty raw -echo; printf '\\033[?1002h\\033[?1006hREADY'; dd bs=1 count=%d of='%s' 2>/dev/null; exec sleep 60", len(want), path)
	p, err := a.sup.SpawnWith(pid, a.cwd, inner.W, inner.H, nil, []string{"/bin/sh", "-c", command})
	if err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "READY")
	m := uv.Mouse{X: inner.X + 2, Y: inner.Y + 2, Button: uv.MouseRight, Mod: uv.ModCtrl}
	for _, kind := range []input.Kind{input.KindMouseClick, input.KindMouseMotion, input.KindMouseRelease} {
		a.onMouse(input.Event{Kind: kind, Mouse: m})
	}
	m.Button = uv.MouseLeft
	a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: m})
	a.onMouse(input.Event{Kind: input.KindMouseRelease, Mouse: m})
	specWaitFile(t, path, []byte(want))
}

func TestStage3ModifiedLeftPressDoesNotSelectHostText(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	pid := a.focusedPane()
	inner := a.innerRect(pid)
	p, err := a.sup.SpawnWith(pid, a.cwd, inner.W, inner.H, nil, []string{"/bin/sh", "-c", "printf READY; exec sleep 60"})
	if err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "READY")
	for _, mod := range []uv.KeyMod{uv.ModShift, uv.ModCtrl, uv.ModAlt} {
		a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: inner.X + 2, Y: inner.Y + 2, Button: uv.MouseLeft, Mod: mod}})
		if a.sel != nil {
			t.Fatalf("modifier %v started host selection", mod)
		}
	}
	a.onMouse(input.Event{Kind: input.KindMouseClick, Mouse: uv.Mouse{X: inner.X + 2, Y: inner.Y + 2, Button: uv.MouseLeft}})
	if a.sel == nil || a.sel.pane != pid || a.sel.ax != 2 {
		t.Fatal("plain press did not start selection", a.sel)
	}
}
