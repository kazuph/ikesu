package runtime

import (
	"io"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
)

func TestPaneSizeQueriesUsePaneGeometry(t *testing.T) {
	s := NewSupervisor()
	p := &Pane{emu: vt.NewEmulator(131, 41), cellWidth: 8, cellHeight: 16}
	s.setupGraphics(p)
	defer p.emu.Close()
	for _, tc := range []struct{ query, want string }{
		{"\x1b[14t", "\x1b[4;656;1048t"},
		{"\x1b[16t", "\x1b[6;16;8t"},
		{"\x1b[18t", "\x1b[8;41;131t"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			response := make(chan string, 1)
			go func() {
				b := make([]byte, len(tc.want))
				_, _ = io.ReadFull(p.emu, b)
				response <- string(b)
			}()
			_, _ = p.emu.Write([]byte(tc.query))
			select {
			case got := <-response:
				if got != tc.want {
					t.Fatalf("query response=%q, want %q", got, tc.want)
				}
			case <-time.After(time.Second):
				t.Fatal("pane did not answer its size query")
			}
		})
	}
}
