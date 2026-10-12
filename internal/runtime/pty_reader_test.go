package runtime

import (
	"io"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

func TestPTYHandoffWakePreservesUnreadOutput(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer slave.Close()
	if _, err := term.MakeRaw(int(slave.Fd())); err != nil {
		t.Fatal(err)
	}
	p := &Pane{}
	if err := p.installPTY(master); err != nil {
		master.Close()
		t.Fatal(err)
	}
	defer p.closePTY()
	if err := p.ptyFile.SetReadDeadline(time.Now()); err == nil {
		t.Fatal("PTY unexpectedly uses Go's deadline poller")
	}
	if _, err := slave.Write([]byte("preserved")); err != nil {
		t.Fatal(err)
	}
	p.pausing.Store(true)
	if _, err := p.wakeWrite.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	if n, err := p.readPTYReady(buf); n != 0 || err != io.EOF {
		t.Fatalf("pause consumed pane output: n=%d err=%v bytes=%q", n, err, buf[:n])
	}
	if _, err := p.wakeRead.Read(make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	p.pausing.Store(false)
	n, err := p.readPTYReady(buf)
	if err != nil || string(buf[:n]) != "preserved" {
		t.Fatalf("resume lost output: %q %v", buf[:n], err)
	}
}
