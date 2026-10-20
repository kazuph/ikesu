package tui

import (
	"github.com/kazuph/goterrace/internal/teststate"
	"os"
	"strings"
	"testing"
	"time"
)

func TestParityScrollbackEditorUsesRealViPTYAndReturnsToShell(t *testing.T) {
	for _, mode := range []string{"local", "server"} {
		t.Run(mode, func(t *testing.T) { testParityScrollbackEditor(t, false, mode == "server") })
	}
}

func TestParityPopupScrollbackEditorKeepsPopupUntilEscape(t *testing.T) {
	for _, mode := range []string{"local", "server"} {
		t.Run(mode, func(t *testing.T) { testParityScrollbackEditor(t, true, mode == "server") })
	}
}

func testParityScrollbackEditor(t *testing.T, popup, server bool) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	a.serverUI = server
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
	if (a.toast == nil) != server {
		t.Fatal("editor launch notification disagrees with local/endpoint observation", server)
	}
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
	// vi may wait for Enter when the temporary path makes its startup
	// message wider than the PTY. Continue the real editor, then inspect it.
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(ep.ScreenText(), "EDITOR_CAPTURE_SENTINEL") {
		if strings.Contains(strings.Join(strings.Fields(ep.ScreenText()), " "), "Press ENTER or type command to continue") {
			a.sup.Input(editor, []byte("\r"))
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("vi startup did not converge", ep.ScreenText())
		}
		time.Sleep(10 * time.Millisecond)
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
