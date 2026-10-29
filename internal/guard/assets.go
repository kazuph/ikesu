package guard

import (
	"encoding/json"
	"fmt"
	"github.com/kazuph/ikesu/internal/childenv"
	"github.com/kazuph/ikesu/internal/filewatch"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Marker struct {
	Pane      string  `json:"pane_id"`
	Agent     string  `json:"agent"`
	Cwd       string  `json:"launch_cwd"`
	Workspace string  `json:"workspace_id"`
	Tab       string  `json:"tab_id"`
	Socket    string  `json:"socket"`
	Mode      string  `json:"mode"`
	Policy    *Config `json:"-"`
}
type Assets struct {
	mu                                      sync.Mutex
	Root, Pane, Bin, MarkerPath, Executable string
	Config                                  Config
	pathWatch                               *filewatch.Watcher
	watchChanges                            func()
	watchInstalled                          bool
	native                                  executableIdentity
	meta                                    Marker
	active                                  string
	closed                                  bool
	injected, prompt                        bool
	mode, unsupported, issue                string
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func NewAssets(root, pane, exe string, c Config) *Assets {
	a := &Assets{Root: root, Pane: pane, Bin: filepath.Join(root, "guard", "bin", pane), MarkerPath: filepath.Join(root, "guard", "agent-panes", pane+".json"), Executable: exe, Config: c}
	if info, err := os.Stat(exe); err == nil {
		a.native = executableFileIdentity(info)
	}
	return a
}
func (a *Assets) Prepare(env []string, cwd string) ([]string, error) {
	m := EnvMap(env)
	a.meta = Marker{Pane: m["IKESU_PANE_ID"], Cwd: cwd, Workspace: m["IKESU_WORKSPACE_ID"], Tab: m["IKESU_TAB_ID"], Socket: m["IKESU_SOCKET_PATH"], Mode: a.Config.EffectiveMode()}
	if !a.Config.Active() {
		return env, nil
	}
	if m["IKESU_PANE_ID"] == "" {
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
	if err := a.validateManaged(true); err != nil {
		return nil, err
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
	a.meta = Marker{Pane: m["IKESU_PANE_ID"], Cwd: cwd, Workspace: m["IKESU_WORKSPACE_ID"], Tab: m["IKESU_TAB_ID"], Socket: m["IKESU_SOCKET_PATH"], Mode: a.Config.EffectiveMode()}
	m["IKESU_GUARD_BIN"] = a.Bin
	m["IKESU_GUARD_MARKER"] = a.MarkerPath
	m["IKESU_GUARD_LAUNCH_CWD"] = cwd
	m["IKESU_GUARD_EXECUTABLE"] = a.RuntimePath()
	if a.Config.EffectiveMode() == "enforce" {
		m["IKESU_GUARD_ENFORCING"] = "1"
	} else {
		delete(m, "IKESU_GUARD_ENFORCING")
	}
	m["PATH"] = a.Bin + string(os.PathListSeparator) + m["PATH"]
	shellDir := filepath.Join(a.Root, "guard", "shell", a.Pane)
	if e := os.MkdirAll(shellDir, 0700); e != nil {
		return nil, e
	}
	if e := a.prepareStartup(m, shellDir); e != nil {
		return nil, e
	}
	a.injected, a.mode = true, a.Config.EffectiveMode()

	return environment(m), nil
}

// Pin identity at the generated entry point, before the CLI sees the caller's
// environment. The socket remains in the marker so handoff can replace it.
func (a *Assets) identity() string {
	return "IKESU_GUARD_MARKER=" + quote(a.MarkerPath) + " IKESU_PANE_ID=" + quote(a.meta.Pane)
}
func (a *Assets) invocation() string {
	return a.identity() + " " + quote(a.RuntimePath())
}
func (a *Assets) shellActive() string {
	if len(a.Config.Commands()) == 0 {
		return "-e " + quote(a.MarkerPath)
	}
	return "-e " + quote(a.MarkerPath) + " || -e " + quote(filepath.Join(a.Bin, a.Config.Commands()[0]))
}
func (a *Assets) shellFunctions() string {
	if len(a.Config.Commands()) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, `_ikesu_guard_forward() {
 if [[ %s ]]; then
  if [[ -n "${BASH_VERSION:-}" && ${_ikesu_guard_reason_name+x} ]]; then
   (
    set +o functrace
%s    %s guard exec --via shell-func -- "$@"
   )
  else %s guard exec --via shell-func -- "$@"; fi
 else builtin command "$@"; fi
}
_ikesu_guard_command() {
 if [[ "${1:-}" != -* ]]; then
 case "${1:-}" in %s) _ikesu_guard_forward "$@"; return $?;; esac
 fi
 builtin command "$@"
}
`, a.shellActive(), bashRequestReasonRestore, a.invocation(), a.invocation(), strings.Join(a.Config.Commands(), "|"))
	for _, cmd := range a.Config.Commands() {
		fmt.Fprintf(&b, `if ! typeset -f %s >/dev/null 2>&1 && ! alias %s >/dev/null 2>&1; then
function %s { _ikesu_guard_forward %s "$@"; }
fi
`, quote(cmd), quote(cmd), cmd, quote(cmd))
	}
	if len(a.Config.Commands()) > 0 {
		b.WriteString(`if ! typeset -f command >/dev/null 2>&1 && ! alias command >/dev/null 2>&1; then
command() { _ikesu_guard_command "$@"; }
fi
`)
	}
	return b.String()
}

func (a *Assets) lineHook() string {
	return fmt.Sprintf(`
if [[ -o interactive ]]; then
 _ikesu_guard_accept_line() {
  if [[ %s ]]; then
   %s guard line -- "$BUFFER" || { zle redisplay; return 0; }
  fi
  zle .accept-line
 }
 if ! zle -l accept-line >/dev/null 2>&1; then zle -N accept-line _ikesu_guard_accept_line; fi
fi
`, a.shellActive(), a.invocation())
}

func (a *Assets) Sync(agent string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	defer a.refreshChanges()
	return a.sync(agent)
}
func (a *Assets) sync(agent string) error {
	if a.closed {
		return nil
	}
	if agent == "" || !a.Config.Active() {
		a.active = ""
		return a.clearCoverage()
	}
	if err := a.validateManaged(true); err != nil {
		return a.setIssue(err.Error())
	}
	reason := a.coverageReason()
	if reason != "" {
		_ = a.remove()
		return a.setIssue(reason)
	}
	if a.active == agent && a.intact() {
		return a.setIssue("")
	}
	if e := os.MkdirAll(a.Bin, 0700); e != nil {
		return a.setIssue(e.Error())
	}
	if e := os.MkdirAll(filepath.Dir(a.MarkerPath), 0700); e != nil {
		return a.setIssue(e.Error())
	}
	if err := os.MkdirAll(filepath.Dir(a.RuntimePath()), 0700); err != nil {
		return a.setIssue(err.Error())
	}
	if err := a.publishRuntime(); err != nil {
		return a.setIssue(err.Error())
	}
	// Publish only after all forwarding entries exist. Repair deleted assets
	// even when the detected agent has not changed.
	for _, cmd := range a.Config.Commands() {
		s := shimScript(a.identity(), a.RuntimePath(), cmd)
		path := filepath.Join(a.Bin, cmd)
		if e := writeGenerated(path, []byte(s), 0700); e != nil {
			_ = a.remove()
			return a.setIssue(e.Error())
		}
		if e := os.Chmod(path, 0700); e != nil {
			_ = a.remove()
			return a.setIssue(e.Error())
		}
	}
	policy := a.Config
	a.meta.Agent, a.meta.Mode, a.meta.Policy = agent, a.Config.EffectiveMode(), &policy
	b, e := json.Marshal(struct {
		Marker
		Policy *Config `json:"guard_config"`
	}{a.meta, &policy})
	if e != nil {
		return a.setIssue(e.Error())
	}
	tmp, e := os.CreateTemp(filepath.Dir(a.MarkerPath), ".marker-")
	if e != nil {
		return a.setIssue(e.Error())
	}
	defer os.Remove(tmp.Name())
	_, e = tmp.Write(b)
	ce := tmp.Close()
	if e != nil {
		return a.setIssue(e.Error())
	}
	if ce != nil {
		return a.setIssue(ce.Error())
	}
	if e = os.Rename(tmp.Name(), a.MarkerPath); e != nil {
		return a.setIssue(e.Error())
	}
	a.active = agent
	return a.setIssue("")
}

func (a *Assets) remove() error {
	if err := a.validateManaged(false); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
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
	a.stopChanges()
	a.active = ""
	a.closed = true
	return a.clearCoverage()
}

// RestoreInherited removes a previous pane's generated shell chain before
// another pane gets its own identity. Its original user environment survives.
func RestoreInherited(env []string) []string {
	return childenv.Scrub(env)
}
