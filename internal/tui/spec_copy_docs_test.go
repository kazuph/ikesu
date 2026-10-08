package tui

import (
	"os"
	"strings"
	"testing"
)

func TestSPECCopyBindingsHelpMenuAndDocs(t *testing.T) {
	a := specRuntimeApp(t)
	check := func() {
		t.Helper()
		for _, row := range a.helpRows() {
			if row.Cmd == "copy mode" {
				if row.Bind != "prefix+[ / prefix+]" {
					t.Fatal("copy bindings", row.Bind)
				}
				return
			}
		}
		t.Fatal("copy missing")
	}
	check()
	a.openMainMenu(0, 0)
	found := false
	for i, item := range a.menu.m.Items {
		if item.Label == "keybinds" {
			a.activateMenu(i)
			found = true
			break
		}
	}
	if !found || a.help == nil {
		t.Fatal("menu help missing")
	}
	check()
	doc, err := os.ReadFile("../../docs/KEYBINDS.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc), "`prefix+[` / `prefix+]`") {
		t.Fatal("documentation lost binding")
	}
}
