package tui

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/kazuph/ikesu/internal/runtime"
	"github.com/kazuph/ikesu/internal/teststate"
)

// The real paste must remain protected for the specified quiet interval.
func TestBracketedPasteHoldsMailOnRealPTY(t *testing.T) {
	teststate.Lock(t)
	a := specRuntimeApp(t)
	capture := filepath.Join(t.TempDir(), "bytes")
	script := fmt.Sprintf("stty raw -echo; printf READY; dd bs=1 count=5 of=%q 2>/dev/null; exec sleep 60", capture)
	p, err := a.sup.SpawnWith(a.focusedPane(), a.cwd, 80, 20, nil, []string{"/bin/sh", "-c", script})
	if err != nil {
		t.Fatal(err)
	}
	specWaitText(t, p, "READY")
	specFeed(a, "\x1b[200~draft\x1b[201~")
	specWaitFile(t, capture, []byte("draft"))
	if _, err := p.WriteMail([]byte("notice\r")); err != runtime.ErrMailInputActive {
		t.Fatalf("paste did not hold mail: %v", err)
	}
	time.Sleep(runtime.MailInputQuiet)
	if _, err := p.WriteMail([]byte("notice\r")); err != nil {
		t.Fatalf("mail did not resume after quiet interval: %v", err)
	}
}
