package client

import (
	"os"
	"reflect"
	"testing"

	"github.com/creack/pty"
	"golang.org/x/term"
)

func TestAttachTerminalRetainsOriginalStateAfterRestoreFailure(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	before, err := term.GetState(int(slave.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	defer term.Restore(int(slave.Fd()), before)
	var terminal attachTerminal
	if err := terminal.enter(int(slave.Fd())); err != nil {
		t.Fatal(err)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	if err := terminal.restore(int(reader.Fd())); err == nil {
		t.Fatal("restore on pipe unexpectedly succeeded")
	}
	if err := terminal.enter(int(slave.Fd())); err != nil {
		t.Fatal(err)
	}
	if err := terminal.restore(int(slave.Fd())); err != nil {
		t.Fatal(err)
	}
	after, err := term.GetState(int(slave.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("original termios was lost after restore failure")
	}
}
