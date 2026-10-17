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
	if a.serverUI || runtime.GOOS != "darwin" || k.Code != 'v' || (k.Mod != uv.ModCtrl && k.Mod != uv.ModSuper) {
		return false
	}
	if !(a.dialog != nil && a.dialog.input != nil || a.navigator != nil || a.cm != nil) {
		return false
	}
	data, err := childenv.Command("/usr/bin/pbpaste").Output()
	if err != nil {
		a.worktreeError(fmt.Errorf("read clipboard: %w", err))
		return true
	}
	a.insertClipboardText(string(data))
	return true
}

func (a *App) insertClipboardText(text string) {
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
