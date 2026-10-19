package server

import (
	"fmt"
	"github.com/kazuph/goterrace/internal/render"
	"github.com/kazuph/goterrace/internal/teststate"
	"strings"
	"testing"
	"time"
	"unicode"
)

func TestEndpointTypedPasteNeverBecomesTerminalKeys(t *testing.T) {
	teststate.Lock(t)
	dir, done := startTestServer(t)
	defer stopServer(t, dir, done)
	c := dialClient(t, dir)
	defer c.conn.Close()
	c.send(t, clientMsg{Type: "hello", Cols: 160, Rows: 45})
	wait := func(t *testing.T, label string, pred func(*render.Input) bool) {
		t.Helper()
		found := false
		c.readUntil(t, 5*time.Second, func(m clientMsg) bool { found = c.lastInput != nil && pred(c.lastInput); return found })
		if !found {
			t.Fatal("missing", label)
		}
	}
	wait(t, "initial state", func(in *render.Input) bool { return in.State != nil })
	key := func(text string) { c.send(t, clientMsg{Type: "input", Data: encode64([]byte(text))}) }
	for _, mode := range []string{"rename", "navigator", "search"} {
		for index, text := range []string{"alpha\x1b[201~\rX", "日本語\x1b[200~x\n\x1b[201~\rX", "before\x1bafter\n\r", "\x1b[201~\x1b[200~\n\r\x1b"} {
			t.Run(fmt.Sprintf("%s/%d", mode, index), func(t *testing.T) {
				switch mode {
				case "rename":
					key("\x02c")
					wait(t, mode, func(in *render.Input) bool { return in.Dialog != nil && in.Dialog.InputEdit })
				case "navigator":
					key("\x02g/")
					wait(t, mode, func(in *render.Input) bool { return in.Navigator != nil && in.Navigator.Searching })
				case "search":
					key("\x02[/")
					wait(t, mode, func(in *render.Input) bool {
						return in.Copy != nil && strings.HasSuffix(in.CopyHint, "  enter search  esc cancel")
					})
				}
				c.send(t, clientMsg{Type: "paste", Data: encode64([]byte(text))})
				wait(t, mode+" body", func(in *render.Input) bool {
					switch mode {
					case "rename":
						return in.Dialog != nil && in.Dialog.Input == text
					case "navigator":
						return in.Navigator != nil && in.Navigator.Searching && in.Navigator.Query == text
					default:
						want := strings.Map(func(r rune) rune {
							if unicode.IsControl(r) {
								return -1
							}
							return r
						}, text)
						return in.Copy != nil && strings.Contains(in.CopyHint, "/"+want+"█")
					}
				})
				// These unambiguous real Escape key sequences cancel only after the
				// entire adversarial body has arrived as one data message.
				key("\x1b[27u\x1b[27u")
				if mode == "search" {
					key("q")
				}
				wait(t, "closed field", func(in *render.Input) bool { return in.Dialog == nil && in.Navigator == nil && in.Copy == nil })
			})
		}
	}
}
