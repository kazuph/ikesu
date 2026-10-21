package client

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/creack/pty"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/teststate"
)

func clearGraphicsFileEnvironment(t *testing.T) {
	t.Helper()
	teststate.Lock(t)
	for _, key := range []string{"SSH_CONNECTION", "SSH_TTY", "TMUX", "STY", "TERM_PROGRAM", "TERM", "KITTY_WINDOW_ID"} {
		teststate.Setenv(t, key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGraphicsFileCapabilityRequiresLocalTerminalAndKnownProgram(t *testing.T) {
	clearGraphicsFileEnvironment(t)
	master, terminal, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer terminal.Close()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	if got := graphicsFileTransport(terminal, terminal, false); got != "" {
		t.Fatal("unidentified terminal advertised direct files", got)
	}
	for _, program := range []string{"ghostty", "GHOSTTY", "WezTerm"} {
		if err := os.Setenv("TERM_PROGRAM", program); err != nil {
			t.Fatal(err)
		}
		if got := graphicsFileTransport(terminal, terminal, false); got != endpoint.DirectKittyTransport {
			t.Fatalf("%s: %s", program, got)
		}
	}
	if graphicsFileTransport(terminal, terminal, true) != "" || graphicsFileTransport(read, terminal, false) != "" || graphicsFileTransport(terminal, write, false) != "" {
		t.Fatal("remote connection or non-TTY advertised local file transport")
	}
	for _, key := range []string{"SSH_CONNECTION", "SSH_TTY", "TMUX", "STY"} {
		if err := os.Setenv(key, ""); err != nil {
			t.Fatal(err)
		}
		if got := graphicsFileTransport(terminal, terminal, false); got != "" {
			t.Fatalf("present %s did not disable direct files: %s", key, got)
		}
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Unsetenv("TERM_PROGRAM"); err != nil {
		t.Fatal(err)
	}
	for _, term := range []string{"xterm-ghostty", "xterm-kitty", "xterm-wezterm"} {
		if err := os.Setenv("TERM", term); err != nil {
			t.Fatal(err)
		}
		if got := graphicsFileTransport(terminal, terminal, false); got != endpoint.DirectKittyTransport {
			t.Fatalf("%s: %s", term, got)
		}
	}
	if err := os.Setenv("TERM", "xterm-256color"); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("KITTY_WINDOW_ID", ""); err != nil {
		t.Fatal(err)
	}
	if got := graphicsFileTransport(terminal, terminal, false); got != endpoint.DirectKittyTransport {
		t.Fatal("existing Kitty window ID did not enable transport", got)
	}
}

func graphicsTransferFixture(t *testing.T) endpoint.GraphicsFile {
	t.Helper()
	path := filepath.Join(t.TempDir(), fmt.Sprintf("herdr-pane-graphics-%d", os.Geteuid()), "server-owned-test", "source", "frame.rgba")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, 24), 0600); err != nil {
		t.Fatal(err)
	}
	return endpoint.GraphicsFile{Path: path, ExpectedLen: 24, ImageID: 17, TransferID: 9,
		Control: "a=t,f=32,s=2,v=3,i=17,q=0"}
}

func TestGraphicsFileWritesOnlyOriginalPathWithSavedCursor(t *testing.T) {
	message := graphicsTransferFixture(t)
	for _, full := range []bool{false, true} {
		if full {
			message.Leading = "\x1b[3;5H"
			message.Control = "a=T,f=32,s=2,v=3,i=17,p=2,c=2,r=3,z=-1,C=1,q=0,x=0,y=0,w=2,h=3,X=0,Y=0"
		}
		read, write, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := writeGraphicsFile(write, message); err != nil {
			t.Fatal(err)
		}
		write.Close()
		got, err := io.ReadAll(read)
		read.Close()
		want := "\x1b7" + message.Leading + "\x1b_G" + message.Control + ",t=f;" + base64.StdEncoding.EncodeToString([]byte(message.Path)) + "\x1b\\\x1b8"
		if err != nil || string(got) != want {
			t.Fatalf("terminal bytes=%q error=%v, want %q", got, err, want)
		}
		if _, err := os.Stat(message.Path); err != nil {
			t.Fatal("terminal transfer removed the producer's source", err)
		}
	}
}

func TestGraphicsFileValidationRejectsMalformedControlWithoutOutput(t *testing.T) {
	for _, name := range []string{"wrong-image", "wrong-length", "duplicate", "wrong-format", "wrong-quiet", "embedded-escape", "unknown-control", "bad-leading", "mode", "symlink"} {
		t.Run(name, func(t *testing.T) {
			message := graphicsTransferFixture(t)
			switch name {
			case "wrong-image":
				message.ImageID++
			case "wrong-length":
				message.ExpectedLen++
			case "duplicate":
				message.Control += ",i=17"
			case "wrong-format":
				message.Control = strings.Replace(message.Control, "f=32", "f=24", 1)
			case "wrong-quiet":
				message.Control = strings.Replace(message.Control, "q=0", "q=2", 1)
			case "embedded-escape":
				message.Control += "\x1b[2J"
			case "unknown-control":
				message.Control += ",t=t"
			case "bad-leading":
				message.Leading = "\x1b[2J"
			case "mode":
				if err := os.Chmod(message.Path, 0644); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				original := message.Path
				message.Path += ".link"
				if err := os.Symlink(original, message.Path); err != nil {
					t.Fatal(err)
				}
			}
			out, err := os.CreateTemp(t.TempDir(), "terminal-output")
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			if err := writeGraphicsFile(out, message); err == nil {
				t.Fatal("accepted malformed local file transfer", name)
			}
			if info, err := out.Stat(); err != nil || info.Size() != 0 {
				t.Fatal("rejected transfer wrote terminal bytes", info, err)
			}
		})
	}
}
