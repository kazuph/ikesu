package client

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"

	uv "github.com/charmbracelet/ultraviolet"
	"golang.org/x/sys/unix"
	"golang.org/x/term"

	"github.com/kazuph/ikesu/internal/input"
	"github.com/kazuph/ikesu/internal/tui"
)

// AttachAgent connects to an already-resolved terminal without starting a
// server or changing the full UI's focus, configuration or endpoint generation.
func AttachAgent(socket, target string, takeover bool, in, out *os.File) (int, error) {
	if !term.IsTerminal(int(in.Fd())) {
		return 1, fmt.Errorf("stdin is not a terminal")
	}
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return 1, err
	}
	defer conn.Close()
	size, err := tui.GetWinsize(in)
	if err != nil {
		return 1, err
	}
	var mu sync.Mutex
	send := func(m msg) error { mu.Lock(); defer mu.Unlock(); return json.NewEncoder(conn).Encode(m) }
	data, _ := json.Marshal(struct {
		Target   string `json:"target"`
		Takeover bool   `json:"takeover"`
	}{target, takeover})
	if err := send(msg{Type: "agent_attach", Data: string(data), Cols: size.Cols, Rows: size.Rows}); err != nil {
		return 1, err
	}
	rd := bufio.NewReader(conn)
	read := func() (msg, error) {
		line, err := rd.ReadBytes('\n')
		if err != nil {
			return msg{}, err
		}
		var m msg
		err = json.Unmarshal(line, &m)
		return m, err
	}
	welcome, err := read()
	if err != nil {
		return 1, err
	}
	if welcome.Type == "error" {
		return 1, fmt.Errorf("%s", welcome.Reason)
	}
	if welcome.Type != "agent_attached" {
		return 1, fmt.Errorf("unsupported agent attach stream")
	}
	old, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return 1, err
	}
	defer term.Restore(int(in.Fd()), old)
	_, err = out.WriteString(tui.EnterSeq + "\x1b[?1000h\x1b[?1006h\x1b[?2004h")
	if err != nil {
		return 1, err
	}
	defer out.WriteString("\x1b[?2004l" + tui.MouseOffSeq + tui.LeaveSeq)
	var detached atomic.Bool
	stop := make(chan struct{})
	finished := make(chan struct{})
	defer func() { close(stop); conn.Close(); <-finished }()
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)
	go func() {
		defer close(finished)
		var framer input.Framer
		keys := agentAttachKeys{rows: size.Rows}
		buf := make([]byte, 4096)
		for {
			select {
			case <-stop:
				return
			default:
			}
			select {
			case <-winch:
				if ws, e := tui.GetWinsize(in); e == nil {
					keys.rows = ws.Rows
					if send(msg{Type: "resize", Cols: ws.Cols, Rows: ws.Rows}) != nil {
						return
					}
				}
			default:
			}
			poll := []unix.PollFd{{Fd: int32(in.Fd()), Events: unix.POLLIN}}
			n, e := unix.Poll(poll, framer.TimeoutMs())
			if e == syscall.EINTR {
				continue
			}
			if e != nil {
				conn.Close()
				return
			}
			var events []input.Event
			if n == 0 {
				if framer.Pending() {
					events = framer.FlushTimeout()
				}
			} else {
				count, e := unix.Read(int(in.Fd()), buf)
				if e != nil || count == 0 {
					conn.Close()
					return
				}
				events = framer.Push(buf[:count])
			}
			for _, event := range events {
				m, detach := keys.event(event)
				if detach {
					detached.Store(true)
					_ = send(msg{Type: "release"})
					conn.Close()
					return
				}
				if m.Type != "" && send(m) != nil {
					conn.Close()
					return
				}
			}
		}
	}()
	for {
		m, err := read()
		if err != nil {
			if detached.Load() {
				return 0, nil
			}
			return 1, fmt.Errorf("server shut down: %w", err)
		}
		switch m.Type {
		case "output":
			data, err := base64.StdEncoding.DecodeString(m.Data)
			if err != nil {
				return 1, err
			}
			if _, err = out.Write(data); err != nil {
				return 1, err
			}
		case "exit":
			return 0, nil
		case "error":
			return 1, fmt.Errorf("%s", m.Reason)
		default:
			return 1, fmt.Errorf("unsupported agent attach message %q", m.Type)
		}
	}
}

type agentAttachKeys struct {
	prefix bool
	rows   int
}

func (k *agentAttachKeys) event(e input.Event) (msg, bool) {
	if e.Kind == input.KindKey {
		isPrefix := e.Key.Code == 'b' && e.Key.Mod == uv.ModCtrl
		if k.prefix {
			k.prefix = false
			if e.Key.Code == 'q' && e.Key.Mod == 0 {
				return msg{}, true
			}
			if isPrefix {
				return msg{Type: "input", Data: base64.StdEncoding.EncodeToString([]byte{2})}, false
			}
		} else if isPrefix {
			k.prefix = true
			return msg{}, false
		}
		if e.Key.Code == uv.KeyPgUp && e.Key.Mod == 0 {
			return msg{Type: "scroll", Rows: k.rows}, false
		}
		if e.Key.Code == uv.KeyPgDown && e.Key.Mod == 0 {
			return msg{Type: "scroll", Rows: -k.rows}, false
		}
	}
	if e.Kind == input.KindMouseWheel {
		if e.Mouse.Button == uv.MouseWheelUp {
			return msg{Type: "scroll", Rows: 1}, false
		}
		if e.Mouse.Button == uv.MouseWheelDown {
			return msg{Type: "scroll", Rows: -1}, false
		}
	}
	if e.Kind == input.KindPaste {
		return msg{Type: "paste", Data: base64.StdEncoding.EncodeToString([]byte(e.Text))}, false
	}
	if e.Kind == input.KindKey {
		return msg{Type: "input", Data: base64.StdEncoding.EncodeToString(e.Raw)}, false
	}
	return msg{}, false
}
