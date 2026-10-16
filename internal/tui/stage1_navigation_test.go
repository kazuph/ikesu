package tui

import (
	"fmt"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/teststate"
)

func TestPopupEscapeRespectsChildModes(t *testing.T) {
	teststate.Lock(t)
	for _, tc := range []struct{ name, mode string }{
		{"plain", ""}, {"alternate", "\x1b[?1049h"}, {"cursor", "\x1b[?1h"},
		{"focus", "\x1b[?1004h"}, {"mouse", "\x1b[?1000h"}, {"kitty", "\x1b[>1u"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := specRuntimeApp(t)
			command := fmt.Sprintf("printf '%%sREADY' '%s'; exec sleep 60", tc.mode)
			if _, err := a.OpenPopup(a.cwd, command, nil, nil); err != nil {
				t.Fatal(err)
			}
			pid := a.focusedPane()
			t.Cleanup(func() { a.sup.Kill(pid) })
			specWaitText(t, a.sup.Get(pid), "READY")
			a.handleKey(uv.Key{Code: uv.KeyEscape, Mod: uv.ModAlt}, []byte("\x1b\x1b"))
			if !a.st.PopupVisible() {
				t.Fatal("Alt+Escape closed popup")
			}
			a.handleKey(uv.Key{Code: uv.KeyEscape}, []byte("\x1b"))
			if a.st.PopupVisible() != (tc.mode != "") {
				t.Fatal("Escape ownership disagrees with child modes")
			}
		})
	}
}

func TestPageKeysScrollOnlyOrdinaryPane(t *testing.T) {
	teststate.Lock(t)
	for _, mode := range []string{"", "\x1b[?1h", "\x1b[?1049h"} {
		t.Run(fmt.Sprintf("%q", mode), func(t *testing.T) {
			a := specRuntimeApp(t)
			pid := a.focusedPane()
			command := `printf '%s' "$1"; i=0; while [ "$i" -lt 120 ]; do printf 'LINE_%s\r\n' "$i"; i=$((i+1)); done; printf READY; exec sleep 60`
			p, err := a.sup.SpawnWith(pid, a.cwd, a.innerRect(pid).W, a.innerRect(pid).H, nil, []string{"/bin/sh", "-c", command, "page", mode})
			if err != nil {
				t.Fatal(err)
			}
			specWaitText(t, p, "READY")
			a.handleKey(uv.Key{Code: uv.KeyPgUp}, []byte("\x1b[5~"))
			want := 0
			if mode == "" {
				want = min(a.innerRect(pid).H, p.Snapshot().ScrollbackLen)
			}
			if a.scroll[pid] != want {
				t.Fatalf("offset=%d want=%d", a.scroll[pid], want)
			}
			a.handleKey(uv.Key{Code: uv.KeyPgDown}, []byte("\x1b[6~"))
			if a.scroll[pid] != 0 {
				t.Fatal("PageDown did not restore live view")
			}
			a.handleKey(uv.Key{Code: uv.KeyPgUp, Mod: uv.ModShift}, []byte("\x1b[5;2~"))
			if a.scroll[pid] != 0 {
				t.Fatal("modified PageUp scrolled host history")
			}
		})
	}
}
