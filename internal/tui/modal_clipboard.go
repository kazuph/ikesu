package tui

import (
	"fmt"
	"runtime"
	"strings"
	"unicode"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/childenv"
	"github.com/kazuph/goterrace/internal/input"
)

func (a *App) modalClipboardKey(k uv.Key) bool {
	if a.serverUI || k.Code != 'v' || (k.Mod != uv.ModCtrl && k.Mod != uv.ModSuper) {
		return false
	}
	if !a.modalTextInput() {
		return false
	}
	read := a.readClipboardText
	if read == nil {
		if runtime.GOOS != "darwin" {
			return false
		}
		read = func() (string, error) {
			data, err := childenv.Command("/usr/bin/pbpaste").Output()
			return string(data), err
		}
	}
	text, err := read()
	if err != nil {
		a.worktreeError(fmt.Errorf("read clipboard: %w", err))
		return true
	}
	a.insertClipboardText(text)
	return true
}

func (a *App) modalTextInput() bool {
	return a.dialog != nil && a.dialog.input != nil || a.navigator != nil && a.navigator.searching || a.cm != nil && a.cm.searching
}

func (a *App) insertClipboardText(text string) {
	if text == "" {
		return
	}
	if a.cm != nil && a.cm.searching {
		text = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, text)
		a.copySearchKey(uv.Key{Text: text})
		return
	}
	a.onInput(input.Event{Kind: input.KindPaste, Text: text})
}
