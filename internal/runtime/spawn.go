package runtime

import (
	"bytes"
	"encoding/base64"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
	"github.com/kazuph/ikesu/internal/childenv"
	"github.com/kazuph/ikesu/internal/guard"
	"os"
	"os/exec"
	"path/filepath"
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
	return s.spawnWith(id, cwd, cols, rows, env, argv, resolved, false, true)
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

	p := s.newPane(id, cwd, cols, rows)
	if benchPaneMonitor != nil {
		benchPaneMonitor(p)
	}
	p.spawnArgv = argv
	emu := p.emu

	var cmd *exec.Cmd
	if len(argv) > 0 {
		cmd = childenv.Command(executionArgv[0], executionArgv[1:]...)
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
		cmd = childenv.Command(shell, args...)

	}
	cmd.Dir = cwd
	env := append(cleanEnv(os.Environ()),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"IKESU=1",
		"IKESU_PANE_ID=p"+itoa(id),
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
			if key != "IKESU_PANE_ID" && key != "GOTERRACE_PANE_ID" {
				filtered = append(filtered, kv)
			}
		}
		cmd.Env = filtered
	}
	if len(withoutPane) == 0 || !withoutPane[0] {
		s.mu.Lock()
		cfg, root := s.guardConfig, s.guardRoot
		s.mu.Unlock()
		if cfg != nil {
			exe, err := os.Executable()
			if err != nil {
				emu.Close()
				return nil, err
			}
			p.guard = guard.NewAssets(root, "p_"+itoa(id), exe, *cfg)
			if len(argv) == 0 {
				p.guard.ConfigureShell(cmd.Path)
			}
			if guard.EnvMap(cmd.Env)["IKESU_PANE_ID"] == "" {
				cmd.Env = append(cmd.Env, "IKESU_PANE_ID=p"+itoa(id))
			}
			cmd.Env, err = p.guard.Prepare(cmd.Env, cwd)
			if err != nil {
				emu.Close()
				return nil, err
			}
			if len(argv) > 0 && (filepath.Base(argv[0]) == "claude" || filepath.Base(argv[0]) == "claude-code") {
				cmd.Env = p.guard.PrepareClaude(cmd.Env)
			}
			if len(withoutPane) > 1 && withoutPane[1] {
				p.guardStarted = filepath.Base(argv[0])
				if err = p.guard.Sync(p.guardStarted); err != nil {
					emu.Close()
					return nil, err
				}
			}
		}
	}
	cmd.Env = childenv.DropLegacy(cmd.Env)
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		if p.guard != nil {
			_ = p.guard.Close()
		}
		emu.Close()
		return nil, err
	}
	p.cmd = cmd
	err = p.installPTY(f)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		emu.Close()
		return nil, err
	}

	p.startPump()
	go p.pumpEmuToPTY()
	go p.waitExit()

	s.mu.Lock()
	s.panes[id] = p
	s.mu.Unlock()
	return p, nil
}

func (s *Supervisor) newPane(id int, cwd string, cols, rows int) *Pane {
	emu := vt.NewEmulator(cols, rows)
	emu.SetScrollbackSize(ScrollbackSize)
	s.mu.Lock()
	if s.scrollbackBytes != nil {
		emu.Scrollback().SetMaxBytes(*s.scrollbackBytes)
	}
	s.mu.Unlock()

	p := &Pane{ID: id, cwd: cwd, emu: emu, ev: s.events, done: make(chan struct{})}
	p.cursor.Visible = true
	s.setupGraphics(p)
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
			if p.graphics != nil {
				p.graphics.Scroll(alt, 0, 0, oldWidth, height+pushed, pulled-pushed)
			}
			if p.read != nil {
				if (p.read.byteHistory || p.read.historyIndexed) && !alt {
					p.read.resizeByteRows(oldWidth, height, pushed, pulled, p.emu.ScrollbackLen())
				} else {
					p.read.resizeRows(alt, oldWidth, height, pushed, pulled)
				}
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

	return p
}
