package tui

import (
	"fmt"
	"path/filepath"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestPopupEscapeTerminalModes(t *testing.T) {
	teststate.Lock(t)
	for _, tc := range []struct {
		name, mode, want string
		closes           bool
	}{
		{"cursor-only", "\x1b[?1h", "", true},
		{"alternate-screen", "\x1b[?1049h", "\x1b", false},
		{"mouse", "\x1b[?1000h", "\x1b", false},
		{"focus", "\x1b[?1004h", "\x1b", false},
		{"keyboard-protocol", "\x1b[>1u", "\x1b[27u", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := specRuntimeApp(t)
			t.Cleanup(a.sup.KillAll)
			capture := filepath.Join(t.TempDir(), "received")
			command := fmt.Sprintf("stty raw -echo; printf '%sREADY'; dd bs=1 count=%d of='%s' 2>/dev/null; exec sleep 60", tc.mode, max(1, len(tc.want)), capture)
			if _, err := a.OpenPopup(a.cwd, command, nil, nil); err != nil {
				t.Fatal(err)
			}
			p := a.sup.Get(a.st.Popup.PaneID)
			specWaitText(t, p, "READY")
			a.handleKey(uv.Key{Code: uv.KeyEscape}, []byte{0x1b})
			if got := !a.st.PopupVisible(); got != tc.closes {
				t.Fatalf("closed=%t, want %t", got, tc.closes)
			}
			if !tc.closes {
				specWaitFile(t, capture, []byte(tc.want))
			}
		})
	}
}

func TestPlainPaneEscapeWithCursorMode(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	t.Cleanup(a.sup.KillAll)
	capture := filepath.Join(t.TempDir(), "received")
	command := fmt.Sprintf("stty raw -echo; printf '\\033[?1hREADY'; dd bs=1 count=1 of='%s' 2>/dev/null; exec sleep 60", capture)
	p, err := a.sup.SpawnWith(a.focusedPane(), a.cwd, 80, 20, nil, []string{"/bin/sh", "-c", command})
	if err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "READY")
	a.handleKey(uv.Key{Code: uv.KeyEscape}, []byte{0x1b})
	specWaitFile(t, capture, []byte("\x1b"))
}
