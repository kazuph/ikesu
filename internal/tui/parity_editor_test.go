package tui

import (
	"github.com/kazuph/goterrace/internal/teststate"
	"os"
	"strings"
	"testing"
	"time"
)

func TestParityScrollbackEditorUsesRealViPTYAndReturnsToShell(t *testing.T) {
	testParityScrollbackEditor(t, false)
}

func TestParityPopupScrollbackEditorKeepsPopupUntilEscape(t *testing.T) {
	testParityScrollbackEditor(t, true)
}

func testParityScrollbackEditor(t *testing.T, popup bool) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	teststate.Setenv(t, "EDITOR", "/usr/bin/vi")
	teststate.Setenv(t, "VISUAL", "")
	teststate.Setenv(t, "TMPDIR", t.TempDir())
	t.Cleanup(a.removeEditorFiles)
	if err := a.StartInitialPane(); err != nil {
		t.Fatal(err)
	}
	original := a.focusedPane()
	p := a.sup.Get(original)
	a.sup.Input(original, []byte("printf 'EDITOR_CAPTURE_SENTINEL\\n'\r"))
	specWaitText(t, p, "EDITOR_CAPTURE_SENTINEL")
	source := original
	if popup {
		if _, err := a.OpenPopup(t.TempDir(), "/bin/sh", nil, nil); err != nil {
			t.Fatal(err)
		}
		source = a.focusedPane()
	}
	if popup {
		a.sup.Input(source, []byte("printf 'POPUP_ONLY_SENTINEL\\n'\r"))
		specWaitText(t, a.sup.Get(source), "POPUP_ONLY_SENTINEL")
	}
	a.editScrollback()
	editor := a.st.FocusedPane()
	if popup {
		if !a.st.PopupVisible() || a.focusedPane() != source {
			t.Fatal("opening editor must retain popup focus until Escape")
		}
		specFeed(a, "\x1b")
		for a.fr.Pending() {
			for _, event := range a.fr.FlushTimeout() {
				a.onInput(event)
			}
		}
		if a.st.PopupVisible() || a.focusedPane() != editor {
			t.Fatal("Escape must uncover the editor pane")
		}
	}
	tab := a.st.ActiveWorkspace().TabRef()
	if editor == original || tab.Zoomed != editor || tab.Layout.PaneCount() != 2 {
		t.Fatal("editor must occupy a new zoomed pane", editor, tab)
	}
	path := a.editorFiles[editor]
	text, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(text), "EDITOR_CAPTURE_SENTINEL") || strings.Contains(string(text), "POPUP_ONLY_SENTINEL") {
		t.Fatal("retained scrollback was not written", err, string(text))
	}
	ep := a.sup.Get(editor)
	if ep == nil {
		t.Fatal("editor has no real PTY")
	}
	specWaitText(t, ep, "EDITOR_CAPTURE_SENTINEL")
	a.sup.Input(editor, []byte("\x1b:q!\r"))
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for tab.Layout.HasPane(editor) {
		select {
		case ev := <-a.sup.Events():
			a.onRuntimeEvent(ev)
		case <-timeout.C:
			t.Fatal("real vi did not exit")
		}
	}
	if a.focusedPane() != original || tab.Zoomed != 0 || a.sup.Get(original) != p {
		t.Fatal("editor exit did not restore original shell")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("editor temp file remains", err)
	}
}
