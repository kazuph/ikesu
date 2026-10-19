package tui

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/teststate"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

func TestModalPasteControlBodiesStayInAllThreeFields(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	capture := filepath.Join(t.TempDir(), "pane-input")
	p, err := a.sup.SpawnWith(a.focusedPane(), a.cwd, 80, 20, nil, []string{"/bin/sh", "-c", fmt.Sprintf(": > %q; stty raw -echo; printf READY; exec cat > %q", capture, capture)})
	if err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "READY")
	path := filepath.Join(t.TempDir(), "clipboard")
	payloads := []string{"alpha\x1b[201~\rX", "日本語\x1b[200~x\n\x1b[201~\rX", "before\x1bafter\n\r", "\x1b[201~\x1b[200~\n\r\x1b"}
	for _, server := range []bool{false, true} {
		for _, mode := range []string{"rename", "navigator", "search"} {
			for i, text := range payloads {
				t.Run(fmt.Sprintf("server=%t/%s/%d", server, mode, i), func(t *testing.T) {
					ui := a
					var viewer *Viewer
					if server {
						viewer = a.NewViewer(io.Discard, 120, 35, nil)
						ui = viewer.app
					}
					name := "default"
					confirmed := 0
					ui.dialog, ui.navigator, ui.cm = nil, nil, nil
					switch mode {
					case "rename":
						ui.dialog = &uiDialog{input: &name, replaceDefault: true, run: func(*App) { confirmed++ }}
					case "navigator":
						ui.navigator = &navigatorView{searching: true}
					case "search":
						ui.cm = &copyMode{searching: true}
					}
					if server {
						viewer.Paste(a, text)
					} else {
						if err := os.WriteFile(path, []byte(text), 0600); err != nil {
							t.Fatal(err)
						}
						ui.readClipboardText = func() (string, error) { b, err := os.ReadFile(path); return string(b), err }
						ui.onInput(input.Event{Kind: input.KindKey, Key: uv.Key{Code: 'v', Mod: uv.ModCtrl}})
					}
					if confirmed != 0 {
						t.Fatal("paste submitted rename")
					}
					switch mode {
					case "rename":
						if ui.dialog == nil || name != text {
							t.Fatalf("rename body=%q want=%q dialog=%v", name, text, ui.dialog)
						}
					case "navigator":
						if ui.navigator == nil || !ui.navigator.searching || ui.navigator.query != text {
							t.Fatal("navigator escaped or lost text")
						}
					case "search":
						want := strings.Map(func(r rune) rune {
							if unicode.IsControl(r) {
								return -1
							}
							return r
						}, text)
						if ui.cm == nil || !ui.cm.searching || ui.cm.query != want {
							t.Fatal("copy search escaped or lost text")
						}
					}
				})
			}
		}
	}
	// This actual pane is a raw byte sink: no clipboard body may reach it,
	// including a delivery whose modal target closed before it arrived.
	v := a.NewViewer(io.Discard, 120, 35, nil)
	v.Paste(a, "outside\x1b[201~\rX")
	// A harness marker proves the raw sink has drained every preceding byte,
	// rather than observing an empty capture before a stray write arrives.
	a.sup.Input(a.focusedPane(), []byte("PASTE_TEST_BARRIER"))
	specWaitFile(t, capture, []byte("PASTE_TEST_BARRIER"))
}
