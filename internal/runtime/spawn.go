package runtime

import (
	"bytes"
	"encoding/base64"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
	"os"
	"os/exec"
	goruntime "runtime"
	"strings"
)

// Spawn starts a login shell in cwd inside a new pane of the given size.
func (s *Supervisor) Spawn(id int, cwd string, cols, rows int) (*Pane, error) {
	return s.SpawnWith(id, cwd, cols, rows, nil, nil)
}

// SpawnWith is Spawn plus per-pane extra environment (API-created panes).
// argv, when non-empty, runs that command instead of the login shell
// (layout.apply command leaves).
func (s *Supervisor) SpawnWith(id int, cwd string, cols, rows int, extra, argv []string) (*Pane, error) {
	return s.spawnWith(id, cwd, cols, rows, extra, argv, argv)
}

// SpawnAgent applies the completed login PATH only to an agent command.
func (s *Supervisor) SpawnAgent(id int, cwd string, cols, rows int, extra, argv []string) (*Pane, error) {
	resolved, path, err := s.ResolveCommand(argv, cwd)
	if err != nil {
		return nil, err
	}
	env := append([]string{"PATH=" + path}, extra...)
	return s.spawnWith(id, cwd, cols, rows, env, argv, resolved)
}

func (s *Supervisor) SpawnPopup(id int, cwd string, cols, rows int, extra, argv []string) (*Pane, error) {
	return s.spawnWith(id, cwd, cols, rows, extra, argv, argv, true)
}

func (s *Supervisor) spawnWith(id int, cwd string, cols, rows int, extra, argv, executionArgv []string, withoutPane ...bool) (*Pane, error) {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if cwd == "" {
		cwd = os.Getenv("HOME")
	}

	emu := vt.NewEmulator(cols, rows)
	emu.SetScrollbackSize(ScrollbackSize)

	p := &Pane{ID: id, cwd: cwd, emu: emu, ev: s.events, done: make(chan struct{})}
	p.spawnArgv = argv
	p.cursor.Visible = true
	emu.RegisterOscHandler(52, func(data []byte) bool {
		// The emulator owns the complete OSC parser; the title observer
		// intentionally truncates text and cannot carry clipboard payloads.
		_, encoded, ok := bytes.Cut(data, []byte(";"))
		if !ok {
			return false
		}
		_, encoded, ok = bytes.Cut(encoded, []byte(";"))
		if !ok || bytes.Equal(encoded, []byte("?")) {
			return false
		}
		if _, err := base64.StdEncoding.DecodeString(string(encoded)); err != nil {
			return false
		}
		p.clipboard = append(p.clipboard, "\x1b]"+string(data)+"\a")
		return true
	})

	emu.SetCallbacks(vt.Callbacks{
		Title:     p.onOSCTitle,
		AltScreen: func(on bool) { p.altScreen = on },
		Resize: func(alt bool, oldWidth, height, pushed, pulled int) {
			if p.read != nil {
				p.read.resizeRows(alt, oldWidth, height, pushed, pulled)
			}
		},
		CursorPosition:   func(_, n uv.Position) { p.cursor.X, p.cursor.Y = n.X, n.Y },
		CursorVisibility: func(v bool) { p.cursor.Visible = v },
		CursorStyle:      func(st vt.CursorStyle, blink bool) { p.cursor.Style, p.cursor.Blink = st, blink },
		WorkingDirectory: func(d string) {
			if d != "" {
				p.procCwd = d
			}
		},
		EnableMode:  func(m ansi.Mode) { p.onMode(m, true) },
		DisableMode: func(m ansi.Mode) { p.onMode(m, false) },
	})

	var cmd *exec.Cmd
	if len(argv) > 0 {
		cmd = exec.Command(executionArgv[0], executionArgv[1:]...)
	} else {
		s.mu.Lock()
		shell, mode := s.defaultShell, s.shellMode
		s.mu.Unlock()
		if shell == "" {
			shell = os.Getenv("SHELL")
		}
		if shell == "" {
			shell = "/bin/sh"
		}
		args := []string{}
		if mode == "login" || mode != "non_login" && goruntime.GOOS == "darwin" {
			args = append(args, "-l")
		}
		cmd = exec.Command(shell, args...)

	}
	cmd.Dir = cwd
	env := append(cleanEnv(os.Environ()),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"GOTERRACE=1",
		"GOTERRACE_PANE_ID="+itoa(id),
	)
	cmd.Env = env
	if s.Env != nil {
		cmd.Env = append(cmd.Env, s.Env(id)...)
	}
	cmd.Env = append(cmd.Env, extra...)
	if len(withoutPane) > 0 && withoutPane[0] {
		filtered := make([]string, 0, len(cmd.Env))
		for _, kv := range cmd.Env {
			key, _, _ := strings.Cut(kv, "=")
			if key != "GTX_PANE_ID" && key != "GOTERRACE_PANE_ID" {
				filtered = append(filtered, kv)
			}
		}
		cmd.Env = filtered
	}
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		emu.Close()
		return nil, err
	}
	p.cmd = cmd
	p.ptyFile = f
	p.ptyFD = int(f.Fd())

	go p.pumpPTYToEmu()
	go p.pumpEmuToPTY()
	go p.waitExit()

	s.mu.Lock()
	s.panes[id] = p
	s.mu.Unlock()
	return p, nil
}
