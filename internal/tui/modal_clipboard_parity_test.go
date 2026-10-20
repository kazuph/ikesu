package tui

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/input"
	"github.com/kazuph/goterrace/internal/teststate"
	"os"
	"path/filepath"
	"testing"
)

func TestStandaloneModalPasteFromTestClipboard(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	path := filepath.Join(t.TempDir(), "clipboard")
	reads := 0
	a.readClipboardText = func() (string, error) { reads++; b, err := os.ReadFile(path); return string(b), err }
	for _, mod := range []uv.KeyMod{uv.ModCtrl, uv.ModSuper} {
		for _, mode := range []string{"rename", "navigator", "search"} {
			name := "default"
			a.dialog, a.navigator, a.cm = nil, nil, nil
			switch mode {
			case "rename":
				a.dialog = &uiDialog{input: &name, replaceDefault: true}
			case "navigator":
				a.navigator = &navigatorView{searching: true}
			case "search":
				a.cm = &copyMode{searching: true}
			}
			for _, text := range []string{"", "日本語\talpha\n"} {
				if err := os.WriteFile(path, []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
				before := reads
				key := uv.Key{Code: 'v', Mod: mod}
				a.onInput(input.Event{Kind: input.KindKey, Key: key})
				a.onInput(input.Event{Kind: input.KindKeyRelease, Key: key})
				if reads != before+1 {
					t.Fatal("release read clipboard", reads, before)
				}
				switch mode {
				case "rename":
					want := text
					if text == "" {
						want = "default"
					}
					if name != want {
						t.Fatal(mode, name, want)
					}
				case "navigator":
					if a.navigator.query != text {
						t.Fatal(mode, a.navigator.query, text)
					}
				case "search":
					want := "日本語alpha"
					if text == "" {
						want = ""
					}
					if a.cm.query != want {
						t.Fatal(mode, a.cm.query, want)
					}
				}
			}
		}
	}
	a.dialog, a.navigator, a.cm = nil, nil, nil
	before := reads
	if a.modalClipboardKey(uv.Key{Code: 'v', Mod: uv.ModCtrl}) || reads != before {
		t.Fatal("nonmodal key consumed")
	}
}
