package runtime

import (
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

func TestPaneInBandResizeCarriesPixelGeometryForBrowserInput(t *testing.T) {
	s := NewSupervisor()
	p := &Pane{emu: vt.NewEmulator(131, 41), cellWidth: 8, cellHeight: 16}
	s.setupGraphics(p)
	defer p.emu.Close()
	p.resizeTerminal(131, 41)
	check := func(want string, action func()) {
		t.Helper()
		response := make(chan string, 1)
		go func() {
			data := make([]byte, 128)
			n, _ := p.emu.Read(data)
			response <- string(data[:n])
		}()
		action()
		select {
		case got := <-response:
			if got != want {
				t.Fatalf("browser resize report=%q, want %q", got, want)
			}
		case <-time.After(time.Second):
			t.Fatal("browser resize report missing")
		}
	}
	check("\x1b[48;41;131;656;1048t", func() {
		_, _ = fmt.Fprintf(p.emu, "\x1b[?%dh", ansi.ModeInBandResize)
	})
	check("\x1b[48;36;111;576;888t", func() { p.resizeTerminal(111, 36) })
	p.cellWidth, p.cellHeight = 16, 32
	check("\x1b[48;36;111;1152;1776t", func() { p.resizeTerminal(111, 36) })
}

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

func TestPaneSizeQueriesUnknownResizeAndKnownRecovery(t *testing.T) {
	s := NewSupervisor()
	p := &Pane{emu: vt.NewEmulator(131, 41)}
	s.setupGraphics(p)
	defer p.emu.Close()
	for _, stage := range []struct {
		name, pixel, cell, dimensions string
		cols, rows, cw, ch            int
	}{
		{"unknown", "\x1b[4;656;1048t", "\x1b[6;16;8t", "\x1b[8;41;131t", 131, 41, 0, 0},
		{"resized", "\x1b[4;576;888t", "\x1b[6;16;8t", "\x1b[8;36;111t", 111, 36, 0, 0},
		{"known", "\x1b[4;1152;1776t", "\x1b[6;32;16t", "\x1b[8;36;111t", 111, 36, 16, 32},
	} {
		t.Run(stage.name, func(t *testing.T) {
			p.cellWidth, p.cellHeight = stage.cw, stage.ch
			p.resizeTerminal(stage.cols, stage.rows)
			response := make(chan string, 1)
			want := stage.pixel + stage.cell + stage.dimensions
			go func() {
				data := make([]byte, len(want))
				_, _ = io.ReadFull(p.emu, data)
				response <- string(data)
			}()
			_, _ = p.emu.Write([]byte("\x1b[14t\x1b[16t\x1b[18t"))
			select {
			case got := <-response:
				if got != want {
					t.Fatalf("got %q, want %q", got, want)
				}
			case <-time.After(time.Second):
				t.Fatal("pane did not answer its size queries")
			}
			if p.cellWidth != stage.cw || p.cellHeight != stage.ch {
				t.Fatal("query defaults changed actual input geometry")
			}
		})
	}
}
