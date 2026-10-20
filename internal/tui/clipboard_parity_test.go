package tui

import (
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/teststate"
	"testing"
	"time"
)

func TestClipboardFeedbackReloadClearsVisibleFlash(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	for _, server := range []bool{false, true} {
		a.serverUI = server
		a.copiedText("first\nsecond\nthird")
		if a.copiedFlash() != "Copied 3 lines" {
			t.Fatal(a.copiedFlash())
		}
		at := a.copiedAt
		if a.copiedFlashAt(at.Add(2*time.Second)) != "" {
			t.Fatal("expired feedback")
		}
		c, err := config.Decode([]byte("[ui.toast.clipboard]\nenabled=false\n"))
		if err != nil {
			t.Fatal(err)
		}
		a.SetSidebarConfig(c, false)
		if a.copiedFlash() != "" || a.copied != "" {
			t.Fatal("disabled flash retained")
		}
		c.UI.Toast.Clipboard.Enabled = nil
		a.SetSidebarConfig(c, false)
		if a.copiedFlash() != "" {
			t.Fatal("old feedback resurrected")
		}
		a.copiedText("one\n")
		if a.copiedFlash() != "Copied 1 line" {
			t.Fatal(a.copiedFlash())
		}
	}
}
