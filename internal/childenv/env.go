// Package childenv separates child contexts from inherited agent sessions.
package childenv

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var markers = map[string]bool{
	"AI_AGENT": true, "CLAUDECODE": true, "CLAUDE_PID": true,
	"CLAUDE_CODE_CHILD_SESSION": true, "CLAUDE_CODE_SESSION_ID": true,
	"CLAUDE_CODE_SESSION_ATTENDED": true, "CLAUDE_CODE_MESSAGING_SOCKET": true,
	"CLAUDE_CODE_MESSAGING_TOKEN": true, "CLAUDE_CODE_ENTRYPOINT": true,
	"CLAUDE_CODE_EXECPATH": true, "CODEX_THREAD_ID": true, "HERDR_AGENT": true, "GTX_AGENT": true, "GOTERRACE_AGENT": true,
}

// Scrub restores proven generated shell settings before dropping pane state.
// Explicit launch extras and fresh guard injection must follow this call.
func Scrub(env []string) []string {
	m := map[string]string{}
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	for _, ns := range []string{"GTX", "HERDR"} {
		prefix := ns + "_GUARD_"
		bin := m[prefix+"BIN"]
		if bin == "" {
			bin = m[prefix+"SHIM_DIR"]
		}
		if bin != "" {
			var paths []string
			for _, p := range filepath.SplitList(m["PATH"]) {
				if filepath.Clean(p) != filepath.Clean(bin) {
					paths = append(paths, p)
				}
			}
			m["PATH"] = strings.Join(paths, string(os.PathListSeparator))
		}
		for _, key := range []string{"ZDOTDIR", "BASH_ENV"} {
			if !guardPath(m[key], key) {
				continue
			}
			orig, exists := m[prefix+key+"_ORIG"]
			if exists && (key != "ZDOTDIR" || m[prefix+key+"_SET"] == "1") && orig != "" {
				m[key] = orig
			} else {
				delete(m, key)
			}
		}
		// Only an exact suffix produced by our startup writer is removed.
		pin := `case "$PATH" in "$` + prefix + `BIN"|"$` + prefix + `BIN":*) ;; *) export PATH="$` + prefix + `BIN:$PATH";; esac` + "\n"
		ready := `if [[ ! -f "$` + prefix + `READY" ]]; then builtin printf '' > "$` + prefix + `READY"; fi` + "\n"
		for _, enforce := range []string{"", "export " + prefix + "ENFORCING=1\n"} {
			suffix := "\n" + pin + enforce + ready
			if strings.HasSuffix(m["PROMPT_COMMAND"], suffix) {
				m["PROMPT_COMMAND"] = strings.TrimSuffix(m["PROMPT_COMMAND"], suffix)
				if m["PROMPT_COMMAND"] == "" && m[prefix+"PROMPT_COMMAND_SET"] == "0" {
					delete(m, "PROMPT_COMMAND")
				}
			}
		}
	}
	for k := range m {
		if markers[k] || strings.HasPrefix(k, "GTX_GUARD_") || strings.HasPrefix(k, "HERDR_GUARD_") && k != "HERDR_GUARD_PANE_SHELL" {
			delete(m, k)
		}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(m))
	for _, k := range keys {
		out = append(out, k+"="+m[k])
	}
	return out
}
func guardPath(path, key string) bool {
	p := filepath.ToSlash(filepath.Clean(path))
	if key == "ZDOTDIR" {
		return strings.HasSuffix(p, "/guard/zdotdir") || strings.Contains(p, "/guard/shell/")
	}
	return strings.HasSuffix(p, "/guard/guard.bash") || strings.Contains(p, "/guard/shell/") && strings.HasSuffix(p, "/bash-env")
}

// ScrubProcess is called before server workers or standalone pane spawns.
func ScrubProcess() {
	old := os.Environ()
	clean := Scrub(old)
	keys := map[string]bool{}
	for _, kv := range clean {
		k, v, _ := strings.Cut(kv, "=")
		keys[k] = true
		_ = os.Setenv(k, v)
	}
	for _, kv := range old {
		k, _, _ := strings.Cut(kv, "=")
		if !keys[k] {
			_ = os.Unsetenv(k)
		}
	}
}
