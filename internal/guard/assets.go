package guard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Marker struct {
	Pane      string `json:"pane_id"`
	Agent     string `json:"agent"`
	Cwd       string `json:"launch_cwd"`
	Workspace string `json:"workspace_id"`
	Tab       string `json:"tab_id"`
	Socket    string `json:"socket"`
	Mode      string `json:"mode"`
}
type Assets struct {
	mu                                      sync.Mutex
	Root, Pane, Bin, MarkerPath, Executable string
	Config                                  Config
	meta                                    Marker
	active                                  string
	closed                                  bool
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func NewAssets(root, pane, exe string, c Config) *Assets {
	return &Assets{Root: root, Pane: pane, Bin: filepath.Join(root, "guard", "bin", pane), MarkerPath: filepath.Join(root, "guard", "agent-panes", pane+".json"), Executable: exe, Config: c}
}
func (a *Assets) Prepare(env []string, cwd string) ([]string, error) {
	if !a.Config.Active() {
		return env, nil
	}
	m := EnvMap(env)
	if m["GTX_PANE_ID"] == "" && m["GOTERRACE_PANE_ID"] == "" {
		return env, nil
	}
	if !has([]string{"record", "enforce"}, a.Config.EffectiveMode()) || a.Config.Timeout() <= 0 {
		return nil, fmt.Errorf("invalid guard mode or timeout")
	}
	for _, cmd := range a.Config.Commands() {
		if cmd == "" || filepath.Base(cmd) != cmd || strings.ContainsAny(cmd, "\n\r\t '$;(){}") {
			return nil, fmt.Errorf("invalid guard target: %q", cmd)
		}
	}
	if e := os.MkdirAll(a.Bin, 0700); e != nil {
		return nil, e
	}
	if e := os.MkdirAll(filepath.Dir(a.MarkerPath), 0700); e != nil {
		return nil, e
	}
	// A newly spawned pane must never inherit a previous pane's marker.
	if e := a.remove(); e != nil {
		return nil, e
	}
	a.meta = Marker{Pane: m["GTX_PANE_ID"], Cwd: cwd, Workspace: m["GTX_WORKSPACE_ID"], Tab: m["GTX_TAB_ID"], Socket: m["GTX_SOCKET_PATH"], Mode: a.Config.EffectiveMode()}
	m["GTX_GUARD_BIN"] = a.Bin
	m["GTX_GUARD_MARKER"] = a.MarkerPath
	m["GTX_GUARD_LAUNCH_CWD"] = cwd
	m["GTX_GUARD_EXECUTABLE"] = a.Executable
	if a.Config.EffectiveMode() == "enforce" {
		m["GTX_GUARD_ENFORCING"] = "1"
	} else {
		delete(m, "GTX_GUARD_ENFORCING")
	}
	m["PATH"] = a.Bin + string(os.PathListSeparator) + m["PATH"]
	shellDir := filepath.Join(a.Root, "guard", "shell", a.Pane)
	if e := os.MkdirAll(shellDir, 0700); e != nil {
		return nil, e
	}
	wrappers := a.shellFunctions()
	// Only .zshenv is intercepted. Reset ZDOTDIR before sourcing the real
	// file so later startup files and history keep their original locations.
	m["GTX_GUARD_ZDOTDIR_ORIG"], m["GTX_GUARD_ZDOTDIR_SET"] = m["ZDOTDIR"], "0"
	if _, ok := m["ZDOTDIR"]; ok {
		m["GTX_GUARD_ZDOTDIR_SET"] = "1"
	}
	zsh := `if [[ "$GTX_GUARD_ZDOTDIR_SET" == 1 ]]; then export ZDOTDIR="$GTX_GUARD_ZDOTDIR_ORIG"; else unset ZDOTDIR; fi
if [[ -f "${ZDOTDIR:-$HOME}/.zshenv" ]]; then source "${ZDOTDIR:-$HOME}/.zshenv"; fi
` + wrappers + zshLineHook
	if e := os.WriteFile(filepath.Join(shellDir, ".zshenv"), []byte(zsh), 0600); e != nil {
		return nil, e
	}
	m["ZDOTDIR"] = shellDir
	m["GTX_GUARD_BASH_ENV_ORIG"] = m["BASH_ENV"]
	bash := `if [[ -n "$GTX_GUARD_BASH_ENV_ORIG" && -f "$GTX_GUARD_BASH_ENV_ORIG" ]]; then source "$GTX_GUARD_BASH_ENV_ORIG"; fi
` + wrappers
	if e := os.WriteFile(filepath.Join(shellDir, "bash-env"), []byte(bash), 0600); e != nil {
		return nil, e
	}
	m["BASH_ENV"] = filepath.Join(shellDir, "bash-env")
	return environment(m), nil
}
func (a *Assets) shellFunctions() string {
	var b strings.Builder
	for _, cmd := range a.Config.Commands() {
		fmt.Fprintf(&b, `if ! typeset -f %s >/dev/null 2>&1 && ! alias %s >/dev/null 2>&1; then
%s() { if [[ -f "$GTX_GUARD_MARKER" ]]; then %s guard exec --via shell-func -- %s "$@"; else builtin command %s "$@"; fi; }
fi
`, quote(cmd), quote(cmd), cmd, quote(a.Executable), quote(cmd), quote(cmd))
	}
	if len(a.Config.Commands()) == 0 {
		return b.String()
	}
	fmt.Fprintf(&b, `if ! typeset -f command >/dev/null 2>&1 && ! alias command >/dev/null 2>&1; then
command() {
 if [[ -f "$GTX_GUARD_MARKER" && "${1:-}" != -* ]]; then
 case "${1:-}" in %s) %s guard exec --via shell-func -- "$@"; return $?;; esac
 fi
 builtin command "$@"
}
fi
`, strings.Join(a.Config.Commands(), "|"), quote(a.Executable))
	return b.String()
}

const zshLineHook = `
if [[ -o interactive ]]; then
 _gtx_guard_accept_line() {
  if [[ -f "$GTX_GUARD_MARKER" ]]; then
   "$GTX_GUARD_EXECUTABLE" guard line -- "$BUFFER" || { zle redisplay; return 0; }
  fi
  zle .accept-line
 }
 if ! zle -l accept-line >/dev/null 2>&1; then zle -N accept-line _gtx_guard_accept_line; fi
fi
`

func (a *Assets) Sync(agent string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || !a.Config.Active() || a.active == agent {
		return nil
	}
	if agent == "" {
		if e := a.remove(); e != nil {
			return e
		}
		a.active = ""
		return nil
	}
	// Populate forwarding entries first, then publish the marker atomically.
	for _, cmd := range a.Config.Commands() {
		s := "#!/bin/sh\nexec " + quote(a.Executable) + " guard exec --via shim -- " + quote(cmd) + " \"$@\"\n"
		if e := os.WriteFile(filepath.Join(a.Bin, cmd), []byte(s), 0700); e != nil {
			a.remove()
			return e
		}
	}
	a.meta.Agent = agent
	b, e := json.Marshal(a.meta)
	if e != nil {
		return e
	}
	tmp, e := os.CreateTemp(filepath.Dir(a.MarkerPath), ".marker-")
	if e != nil {
		return e
	}
	defer os.Remove(tmp.Name())
	_, e = tmp.Write(b)
	ce := tmp.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if e = os.Rename(tmp.Name(), a.MarkerPath); e != nil {
		return e
	}
	a.active = agent
	return nil
}
func (a *Assets) remove() error {
	if e := os.Remove(a.MarkerPath); e != nil && !os.IsNotExist(e) {
		return e
	}
	entries, e := os.ReadDir(a.Bin)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	for _, f := range entries {
		if e = os.Remove(filepath.Join(a.Bin, f.Name())); e != nil {
			return e
		}
	}
	return nil
}
func (a *Assets) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.active = ""
	a.closed = true
	return a.remove()
}
