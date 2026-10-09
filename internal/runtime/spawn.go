package runtime

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
	"os"
	"os/exec"
)

// Spawn starts a login shell in cwd inside a new pane of the given size.
func (s *Supervisor) Spawn(id int, cwd string, cols, rows int) (*Pane, error) {
	return s.SpawnWith(id, cwd, cols, rows, nil, nil)
}

// SpawnWith is Spawn plus per-pane extra environment (API-created panes).
// argv, when non-empty, runs that command instead of the login shell
// (layout.apply command leaves).
func (s *Supervisor) SpawnWith(id int, cwd string, cols, rows int, extra, argv []string) (*Pane, error) {
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

	emu.SetCallbacks(vt.Callbacks{
		Title:            p.onOSCTitle,
		AltScreen:        func(on bool) { p.altScreen = on },
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
	var loginPath string
	if len(argv) > 0 {
		resolved, path, err := s.ResolveCommand(argv, cwd)
		if err != nil {
			emu.Close()
			return nil, err
		}
		loginPath = path
		cmd = exec.Command(resolved[0], resolved[1:]...)
	} else {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/zsh"
		}
		cmd = exec.Command(shell, "-l")
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
	if len(argv) > 0 {
		cmd.Env = append(cmd.Env, "PATH="+loginPath)
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
