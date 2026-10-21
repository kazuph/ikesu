package tui

import (
	"testing"

	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/teststate"
)

func TestStage6bModalPaste(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	name := "12"
	a.dialog = &uiDialog{input: &name, replaceDefault: true}
	a.onInput(input.Event{Kind: input.KindPaste})
	if name != "12" || !a.dialog.replaceDefault {
		t.Fatal("empty paste changed rename default")
	}
	a.onInput(input.Event{Kind: input.KindPaste, Text: "日本語"})
	if name != "日本語" || a.dialog.replaceDefault {
		t.Fatal("paste must replace default", name)
	}
	a.onInput(input.Event{Kind: input.KindPaste, Text: " alpha"})
	if name != "日本語 alpha" {
		t.Fatal("second paste must append", name)
	}
	a.dialog = nil
	a.navigator = &navigatorView{searching: true, query: "pa"}
	a.onInput(input.Event{Kind: input.KindPaste, Text: "ne"})
	if a.navigator.query != "pane" {
		t.Fatal("navigator dropped paste")
	}
	a.navigator = nil
	a.cm = &copyMode{searching: true, query: "al"}
	a.onInput(input.Event{Kind: input.KindPaste, Text: "pha\t\x01beta\n"})
	if a.cm.query != "alphabeta" {
		t.Fatal("copy search dropped or failed to filter bracketed paste", a.cm.query)
	}
	a.cm.query = "al"
	a.insertClipboardText("pha\t\x01beta\n")
	if a.cm.query != "alphabeta" {
		t.Fatalf("copy query = %q", a.cm.query)
	}
}
