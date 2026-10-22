package childenv

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, v := range env {
		k, x, _ := strings.Cut(v, "=")
		m[k] = x
	}
	return m
}
func TestSessionMarkersAndGuardStateAreRemoved(t *testing.T) {
	env := []string{"PATH=/bin", "CLAUDE_CONFIG_DIR=/config", "CODEX_HOME=/codex", "CLAUDE_EFFORT=high", "CLAUDE_CODE_ENABLE_TASKS=1", "CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING=1", "BASH_ENV=/user/bash", "ZDOTDIR=/user/zsh", "PROMPT_COMMAND=user hook", "IKESU_GUARD_MARKER=/old", "HERDR_GUARD_READY=1"}
	for k := range markers {
		env = append(env, k+"=inherited")
	}
	clean := envMap(Scrub(env))
	for k := range markers {
		if _, ok := clean[k]; ok {
			t.Fatal(k)
		}
	}
	for _, k := range []string{"IKESU_GUARD_MARKER", "HERDR_GUARD_READY"} {
		if _, ok := clean[k]; ok {
			t.Fatal(k)
		}
	}
	for k, v := range envMap(env[:9]) {
		if clean[k] != v {
			t.Fatal(k, clean[k], v)
		}
	}
	// Exercise the actual OS child environment, then deliberate extras.
	cmd := exec.Command("/usr/bin/env")
	cmd.Env = append(Scrub(env), "CODEX_THREAD_ID=explicit")
	data, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "CODEX_THREAD_ID=explicit\n") || strings.Contains(string(data), "CLAUDE_CODE_CHILD_SESSION=") {
		t.Fatal(string(data))
	}
}
func TestGuardShellValuesRequireProof(t *testing.T) {
	for _, path := range []string{"/user/zsh", "/tmp/guard/shell-other/user", "/tmp/guard/shell/user"} {
		m := envMap(Scrub([]string{"ZDOTDIR=" + path, "IKESU_GUARD_BIN=/tmp/guard/bin/p1", "IKESU_GUARD_ZDOTDIR_ORIG=/old", "IKESU_GUARD_ZDOTDIR_SET=1"}))
		if m["ZDOTDIR"] != path {
			t.Fatal(m)
		}
	}
	m := envMap(Scrub([]string{"BASH_ENV=/tmp/guard/shell/p_1/bash-env", "IKESU_GUARD_BASH_ENV_ORIG=/user/bash", "ZDOTDIR=/tmp/guard/shell/p_1", "IKESU_GUARD_ZDOTDIR_ORIG=/user/zsh", "IKESU_GUARD_ZDOTDIR_SET=1"}))
	if m["BASH_ENV"] != "/user/bash" || m["ZDOTDIR"] != "/user/zsh" {
		t.Fatal(m)
	}
}
func TestProcessScrubBeforeChildren(t *testing.T) {
	t.Setenv("CLAUDE_CODE_CHILD_SESSION", "1")
	t.Setenv("IKESU_GUARD_ENFORCING", "1")
	t.Setenv("CLAUDE_EFFORT", "high")
	ScrubProcess()
	if os.Getenv("CLAUDE_CODE_CHILD_SESSION") != "" || os.Getenv("IKESU_GUARD_ENFORCING") != "" || os.Getenv("CLAUDE_EFFORT") != "high" {
		t.Fatal("process environment")
	}
}

func TestGeneratedPromptSuffixKeepsUserLines(t *testing.T) {
	user := "echo user\nprintf user"
	pin := `case "$PATH" in "$IKESU_GUARD_BIN"|"$IKESU_GUARD_BIN":*) ;; *) export PATH="$IKESU_GUARD_BIN:$PATH";; esac` + "\n"
	ready := `if [[ ! -f "$IKESU_GUARD_READY" ]]; then builtin printf '' > "$IKESU_GUARD_READY"; fi` + "\n"
	for _, enforce := range []string{"", "export IKESU_GUARD_ENFORCING=1\n"} {
		m := envMap(Scrub([]string{"PROMPT_COMMAND=" + user + "\n" + pin + enforce + ready, "IKESU_GUARD_PROMPT_COMMAND_SET=1"}))
		if m["PROMPT_COMMAND"] != user {
			t.Fatal(m)
		}
	}
	m := envMap(Scrub([]string{"PROMPT_COMMAND=" + user + "\n" + pin + ready + "echo user-added\n", "IKESU_GUARD_PROMPT_COMMAND_SET=1"}))
	if m["PROMPT_COMMAND"] != user+"\n"+pin+ready+"echo user-added\n" {
		t.Fatal("unproven user suffix removed")
	}
}
