package guard

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mvdan.cc/sh/v3/syntax"
)

func bootstrapForTest(snapshot, payload, cwd string) string {
	s := strings.Replace(claudeBootstrap, "IKESU_SNAPSHOT", quote(snapshot), 1)
	s = strings.Replace(s, "IKESU_PAYLOAD", quote(payload), 1)
	return strings.Replace(s, "IKESU_CWD", quote(cwd), 1)
}

func TestClaudePrefixRecognizesOnlyCanonicalBootstrap(t *testing.T) {
	i := Input{Env: map[string]string{"HOME": "/tmp/test-user"}}
	snapshot := "/tmp/test-user/.claude/shell-snapshots/snapshot-zsh-123-x.sh"
	for _, payload := range []string{"ls", "git status", "cd /tmp; pwd", "printf '%s' ok | cat", `echo "$HOME"`, "false || /bin/rm '/tmp/victim'", "env -i /bin/rm /tmp/victim"} {
		line := bootstrapForTest(snapshot, payload, "/tmp/claude-123-cwd")
		for _, candidate := range []string{line, strings.TrimPrefix(line, "source "+quote(snapshot)+" 2>/dev/null || true && ")} {
			if got := ClaudePrefixLine(i, candidate); got != payload {
				t.Fatalf("payload=%q got=%q", payload, got)
			}
		}
	}
	good := bootstrapForTest(snapshot, "ls", "/tmp/claude-123-cwd")
	for name, line := range map[string]string{
		"extra command":         good + "; /bin/rm /tmp/victim",
		"glob snapshot":         strings.Replace(good, quote(snapshot), strings.Replace(snapshot, "snapshot-zsh-123-x.sh", "snapshot-zsh-*.sh", 1), 1),
		"different snapshot":    strings.Replace(good, snapshot, "/tmp/untrusted.sh", 1),
		"different destination": strings.Replace(good, "/tmp/claude-123-cwd", "/tmp/victim", 1),
		"changed settings":      strings.Replace(good, "shopt -u extglob", "shopt -u extdebug", 1),
		"expanded eval":         strings.Replace(good, "eval 'ls'", `eval "$PAYLOAD"`, 1),
		"negated":               "! " + good,
		"extra eval":            strings.Replace(good, "eval 'ls'", "eval 'ls' && eval '/bin/rm /tmp/victim'", 1),
		"plain eval":            "eval ls",
		"hook":                  "node ~/.claude/hooks/stop-require-japanese.js",
		"syntax error":          "eval '",
	} {
		t.Run(name, func(t *testing.T) {
			for _, candidate := range []string{line, strings.Replace(line, " < /dev/null", "", 1)} {
				if got := ClaudePrefixLine(i, candidate); got != candidate {
					t.Fatalf("unexpected normalization %q", got)
				}
			}
		})
	}
	i.Env["CLAUDE_CONFIG_DIR"] = "/tmp/custom-claude"
	custom := bootstrapForTest("/tmp/custom-claude/shell-snapshots/snapshot-zsh-1-a.sh", "ls", "/tmp/claude-1-cwd")
	if got := ClaudePrefixLine(i, custom); got != "ls" {
		t.Fatal(got)
	}
}

func TestClaudePrefixWithoutDevNull(t *testing.T) {
	i := Input{Cwd: "/tmp/work", LaunchCwd: "/tmp/work", Env: map[string]string{"HOME": "/tmp/test-user"}}
	snapshot := "/tmp/test-user/.claude/shell-snapshots/snapshot-zsh-123-x.sh"
	payloads := []string{
		"ls",
		`git commit -q -m "x" -m "Co-Authored-By: A <a@b.c>"`,
		`AI_GUARD_REQUEST_REASON='対象プロジェクト: dotfiles。必要な理由: …。実施内容: …。確認済み: …。戻し方: …。' git commit -q -m "msg" -m "Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"`,
		"dd if=/dev/zero of=/tmp/victim",
		"ikesu send '*' hello",
		"eval ls",
	}
	for _, payload := range payloads {
		for _, withSource := range []bool{true, false} {
			for _, withDevNull := range []bool{true, false} {
				line := bootstrapForTest(snapshot, payload, "/tmp/claude-123-cwd")
				if !withSource {
					line = strings.TrimPrefix(line, "source "+quote(snapshot)+" 2>/dev/null || true && ")
				}
				if !withDevNull {
					line = strings.Replace(line, " < /dev/null", "", 1)
				}
				t.Run(payload+fmt.Sprint(withSource, withDevNull), func(t *testing.T) {
					normalized := ClaudePrefixLine(i, line)
					_, got, err := lineVerdict(Config{}, i, normalized)
					_, want, wantErr := lineVerdict(Config{}, i, payload)
					t.Logf("source=%v devnull=%v decision=%s rule=%s reason=%s", withSource, withDevNull, got.Decision, got.Rule, got.Reason)
					if normalized != payload || err != nil || wantErr != nil || got.Decision != want.Decision || got.Rule != want.Rule {
						t.Fatalf("normalization=%q verdict=%+v want=%+v errors=%v/%v", normalized, got, want, err, wantErr)
					}
				})
			}
		}
	}
	good := bootstrapForTest(snapshot, "ls", "/tmp/claude-123-cwd")
	for _, withSource := range []bool{true, false} {
		for _, redirect := range []string{" < /etc/passwd", " <<< 'ls'", " < /dev/null < /etc/passwd", " 3< /dev/null"} {
			line := strings.Replace(good, " < /dev/null", redirect, 1)
			if !withSource {
				line = strings.TrimPrefix(line, "source "+quote(snapshot)+" 2>/dev/null || true && ")
			}
			if got := ClaudePrefixLine(i, line); got != line {
				t.Fatalf("accepted redirect %q: %q", redirect, got)
			}
		}
	}
}

func TestClaudePrefixMultiline(t *testing.T) {
	i := Input{Env: map[string]string{"HOME": "/tmp/test-user"}}
	snapshot := "/tmp/test-user/.claude/shell-snapshots/snapshot-zsh-123-x.sh"
	payload := "printf '%s\n' \"quoted\"\ncat <<'EOF'\nhello 'world'\nEOF"
	for _, withSource := range []bool{true, false} {
		for _, withDevNull := range []bool{true, false} {
			line := bootstrapForTest(snapshot, payload, "/tmp/claude-123-cwd")
			if !withSource {
				line = strings.TrimPrefix(line, "source "+quote(snapshot)+" 2>/dev/null || true && ")
			}
			if !withDevNull {
				line = strings.Replace(line, " < /dev/null", "", 1)
			}
			t.Run(fmt.Sprint(withSource, withDevNull), func(t *testing.T) {
				if got := ClaudePrefixLine(i, line); got != payload {
					t.Fatalf("multiline payload not extracted: %q", got)
				}
			})
		}
	}
}

func TestClaudePrefixMultilineRejectsChanges(t *testing.T) {
	i := Input{Env: map[string]string{"HOME": "/tmp/test-user"}}
	snapshot := "/tmp/test-user/.claude/shell-snapshots/snapshot-zsh-123-x.sh"
	payload := "cat <<'EOF'\nhello 'world'\nEOF"
	good := bootstrapForTest(snapshot, payload, "/tmp/claude-123-cwd")
	changes := map[string]string{
		"extra command":         good + "; /bin/rm /tmp/victim",
		"glob snapshot":         strings.Replace(good, quote(snapshot), "/tmp/test-user/.claude/shell-snapshots/snapshot-zsh-*.sh", 1),
		"different snapshot":    strings.Replace(good, snapshot, "/tmp/untrusted.sh", 1),
		"different destination": strings.Replace(good, "/tmp/claude-123-cwd", "/tmp/victim", 1),
		"changed settings":      strings.Replace(good, "shopt -u extglob", "shopt -u extdebug", 1),
		"expanded eval":         strings.Replace(good, quote(payload), quote(payload)+`"$PAYLOAD"`, 1),
		"negated":               "! " + good,
		"extra eval":            strings.Replace(good, "eval "+quote(payload), "eval "+quote(payload)+" && eval 'ls'", 1),
	}
	for name, line := range changes {
		for _, devnull := range []bool{true, false} {
			if !devnull {
				line = strings.Replace(line, " < /dev/null", "", 1)
			}
			t.Run(name+fmt.Sprint(devnull), func(t *testing.T) {
				if got := ClaudePrefixLine(i, line); got != line {
					t.Fatalf("accepted changed bootstrap: %q", got)
				}
			})
		}
	}
	for _, redirect := range []string{" < /etc/passwd", " <<< 'ls'", " < /dev/null < /etc/passwd", " 3< /dev/null"} {
		for _, source := range []bool{true, false} {
			line := strings.Replace(good, " < /dev/null", redirect, 1)
			if !source {
				line = strings.TrimPrefix(line, "source "+quote(snapshot)+" 2>/dev/null || true && ")
			}
			if got := ClaudePrefixLine(i, line); got != line {
				t.Fatalf("accepted redirect %q", redirect)
			}
		}
	}
}

func TestClaudePrefixRealMultiline(t *testing.T) {
	data, err := os.ReadFile("testdata/claude-multiline.txt")
	if err != nil {
		t.Fatal(err)
	}
	line := string(data)
	tree, err := syntax.NewParser().Parse(strings.NewReader(line), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	var payload string
	syntax.Walk(tree, func(n syntax.Node) bool {
		if call, ok := n.(*syntax.CallExpr); ok && len(call.Args) == 2 {
			if name, _ := claudeLiteral(call.Args[0]); name == "eval" {
				payload, _ = claudeLiteral(call.Args[1])
			}
		}
		return true
	})
	if !strings.Contains(payload, "\n") || !strings.Contains(payload, "<<'EOF'") {
		t.Fatal("fixture lost multiline heredoc")
	}
	if got := ClaudePrefixLine(Input{}, line); got != payload {
		t.Fatal("real launch line not extracted")
	}
}

func TestClaudePrefixEnvironmentIsScopedAndRestored(t *testing.T) {
	for _, old := range []string{"", "custom-prefix --unchanged"} {
		for _, present := range []bool{false, true} {
			a := NewAssets(t.TempDir(), "p_1", "/usr/bin/true", Config{})
			input := []string{"HOME=" + t.TempDir(), "PATH=/usr/bin:/bin", "IKESU_PANE_ID=p1"}
			if present {
				input = append(input, "CLAUDE_CODE_SHELL_PREFIX="+old)
			}
			env, e := a.Prepare(input, "/tmp")
			if e != nil {
				t.Fatal(e)
			}
			defer a.Close()
			if present && EnvMap(env)["CLAUDE_CODE_SHELL_PREFIX"] != old {
				t.Fatal("ordinary pane prefix changed")
			}
			prepared := a.PrepareClaude(env)
			prefix := EnvMap(prepared)["CLAUDE_CODE_SHELL_PREFIX"]
			want := filepath.Join(a.Root, "guard", "shell", a.Pane, "claude-prefix")
			if prefix != want {
				t.Fatal(prefix, want)
			}
			got, exists := EnvMap(RestoreInherited(prepared))["CLAUDE_CODE_SHELL_PREFIX"]
			if exists != present || present && got != old {
				t.Fatal(got, exists, old, present)
			}
		}
	}
}

func TestClaudePrefixRemovalRequiresAsk(t *testing.T) {
	i := Input{Cwd: "/tmp/work", LaunchCwd: "/tmp/work", Env: map[string]string{"HOME": "/tmp/home", "CLAUDE_CODE_SHELL_PREFIX": "/tmp/prefix", "KEY": "CLAUDE_CODE_SHELL_PREFIX"}}
	for _, line := range []string{
		"env -u CLAUDE_CODE_SHELL_PREFIX /tmp/claude --print hello",
		"env --unset=CLAUDE_CODE_SHELL_PREFIX /tmp/claude-code hello",
		"env -uCLAUDE_CODE_SHELL_PREFIX /tmp/claude hello",
		"env -i /tmp/claude hello", "exec -c /tmp/claude hello",
		"CLAUDE_CODE_SHELL_PREFIX= /tmp/claude hello",
		"CLAUDE_CODE_SHELL_PREFIX=/tmp/other command /tmp/claude hello",
		"env CLAUDE_CODE_SHELL_PREFIX= /tmp/claude hello",
		"command env --unset CLAUDE_CODE_SHELL_PREFIX /tmp/claude hello",
		"env -S '-u CLAUDE_CODE_SHELL_PREFIX /tmp/claude' hello",
		"unset -v CLAUDE_CODE_SHELL_PREFIX; claude hello",
		"export CLAUDE_CODE_SHELL_PREFIX=; /tmp/claude hello",
		"export -n CLAUDE_CODE_SHELL_PREFIX; claude hello",
		"env -i /tmp/codex hello", "CLAUDE_CODE_SHELL_PREFIX= /bin/echo ordinary",
		"ikesu guard claude-prefix -- /bin/sh -c '/bin/rm /tmp/home/../victim' ls",
		"command ikesu guard claude-prefix -- /bin/rm /tmp/home/../victim",
		"env ikesu guard claude-prefix -- /bin/zsh -c 'touch /tmp/victim' echo",
	} {
		t.Run(line, func(t *testing.T) {
			_, v, _ := lineVerdict(Config{}, i, line)
			if v.Decision != "ask" {
				t.Fatalf("%s: %+v", line, v)
			}
		})
	}
	for _, line := range []string{"claude hello", "/tmp/claude --print hello", "env TEST=ok /tmp/claude hello", "ls", "git status", "echo ordinary | cat"} {
		t.Run(line, func(t *testing.T) {
			_, v, e := lineVerdict(Config{}, i, line)
			if e != nil || v.Decision != "allow" {
				t.Fatalf("%s: %+v %v", line, v, e)
			}
		})
	}
}

func TestClaudePrefixChangesDoNotDependOnExecutableName(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "claude")
	if e := os.WriteFile(target, []byte("#!/bin/sh\nexit 99\n"), 0700); e != nil {
		t.Fatal(e)
	}
	alias := filepath.Join(dir, "assistant")
	if e := os.Symlink(target, alias); e != nil {
		t.Fatal(e)
	}
	i := Input{Cwd: dir, LaunchCwd: dir, Env: map[string]string{"HOME": dir, "KEY": "CLAUDE_CODE_SHELL_PREFIX", "CLAUDE_CODE_SHELL_PREFIX": "/tmp/prefix"}}
	lines := []string{
		"env -u CLAUDE_CODE_SHELL_PREFIX npx @anthropic-ai/claude-code --print hello",
		"env -S '-u CLAUDE_CODE_SHELL_PREFIX npx @anthropic-ai/claude-code' --print hello",
		"env -u CLAUDE_CODE_SHELL_PREFIX " + quote(alias) + " --print hello",
		"CLAUDE_CODE_SHELL_PREFIX= exec " + quote(alias) + " --print hello",
		"env --ignore-environment echo ordinary", "env - echo ordinary",
		"env CLAUDE_CODE_SHELL_PREFIX=/tmp/other /bin/echo ordinary",
		"unsetenv CLAUDE_CODE_SHELL_PREFIX", "setenv CLAUDE_CODE_SHELL_PREFIX /tmp/other",
		`unset "$KEY"`, `unsetenv "$KEY"`,
		`CLAUDE_CODE_SHELL_PREFIX="" /bin/echo ordinary`,
		`CLAUDE_CODE_SHELL_PREFIX="/tmp/"other /bin/echo ordinary`,
		`env CLAUDE_CODE_SHELL_PREFIX="" npx --version`,
		"builtin unset CLAUDE_CODE_SHELL_PREFIX", "builtin export \"CLAUDE_CODE_SHELL_PREFIX=\"",
		"export -n \"$KEY\"", "typeset +x \"$KEY\"",
	}
	for _, declaration := range []string{"export", "typeset", "declare", "readonly"} {
		for _, value := range []string{`"CLAUDE_CODE_SHELL_PREFIX="`, `"CLAUDE_CODE_SHELL_PREFIX=/tmp/other"`, `"$KEY="`, `"${KEY}=/tmp/other"`, `'CLAUDE_CODE_'"SHELL_PREFIX="`, `"CLAUDE_CODE_SHELL_PREFIX"'=/tmp/other'`} {
			lines = append(lines, declaration+" "+value+"; command claude hello")
		}
	}
	for _, line := range lines {
		t.Run(line, func(t *testing.T) {
			_, v, _ := lineVerdict(Config{}, i, line)
			if v.Decision != "ask" {
				t.Fatalf("%s: %+v", line, v)
			}
			var out bytes.Buffer
			if !AuthorizeLine(Config{}, dir, i, line, &out, nil) {
				t.Fatalf("ordinary pane unexpectedly guarded: %s", out.String())
			}
		})
	}
	for _, line := range []string{"echo ordinary", "ls", "git status", "codex hello", "env TEST=ok codex hello", "export TEST=ordinary", `export "TEST=ordinary"`, "typeset TEST=ok", "readonly TEST=ok", "declare TEST=ok", "env -u TEST echo ordinary", "TEST=ok npx --version"} {
		t.Run(line, func(t *testing.T) {
			_, v, e := lineVerdict(Config{}, i, line)
			if e != nil || v.Decision != "allow" {
				t.Fatalf("%s: %+v %v", line, v, e)
			}
		})
	}
}
