package guard

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
	"mvdan.cc/sh/v3/syntax"
)

// These limits are the approved plan's resource bounds, shared by nested files.
const scriptFileLimit = 256 * 1024
const scriptTotalLimit = 1024 * 1024
const scriptDepthLimit = 3

type scriptKind uint8

const (
	scriptSafe scriptKind = iota
	scriptUnresolved
	scriptUnparseable
	scriptDanger
	scriptBlocked
)

type scriptResult struct {
	kind    scriptKind
	verdict Verdict
	trigger string
}

func (r scriptResult) merge(other scriptResult) scriptResult {
	if other.kind > r.kind {
		return other
	}
	return r
}
func scriptIssue(k scriptKind, reason string) scriptResult {
	return scriptResult{kind: k, verdict: result("ask", "shell-script", reason)}
}

type scriptScanner struct {
	config Config
	total  int
	stack  []os.FileInfo
}

type unresolvedScriptError struct{ text string }

func (e unresolvedScriptError) Error() string { return e.text }
func scriptError(e error) scriptResult {
	var unresolved unresolvedScriptError
	if errors.As(e, &unresolved) {
		return scriptIssue(scriptUnresolved, e.Error())
	}
	return scriptIssue(scriptUnparseable, e.Error())
}

// Read options only before the program operand. The remainder belongs to it.
func shellFileArgument(args []string) (string, bool, bool) {
	for n := 0; n < len(args); n++ {
		a := args[n]
		if a == "--" {
			if n+1 < len(args) {
				return args[n+1], true, false
			}
			return "", false, true
		}
		if a == "-" {
			return "", false, false
		}
		if a == "-o" || a == "+o" {
			n++
			if n >= len(args) || !has([]string{"pipefail", "errexit", "nounset", "xtrace"}, args[n]) {
				return "", false, false
			}
			continue
		}
		if a == "--norc" || a == "--noprofile" {
			continue
		}
		if strings.HasPrefix(a, "-") || strings.HasPrefix(a, "+") {
			if !strings.HasPrefix(a, "-") || len(a) == 1 {
				return "", false, false
			}
			for _, c := range a[1:] {
				if !strings.ContainsRune("euxv", c) {
					return "", false, false
				}
			}
			continue
		}
		return a, true, false
	}
	return "", false, true
}

func (s *scriptScanner) file(i Input, name string) scriptResult {
	fail := func(reason string) scriptResult { return scriptIssue(scriptUnparseable, name+": "+reason) }
	if i.Unresolved || name == "" || name == "-" || strings.HasPrefix(name, "/dev/fd/") || name == "/dev/stdin" || strings.HasPrefix(name, "/proc/self/fd/") {
		return fail("実行するファイルを確定できません")
	}
	if !filepath.IsAbs(name) {
		if i.CwdUncertain {
			return fail("ディレクトリ変更後のファイルを確定できません")
		}
		name = filepath.Join(i.Cwd, name)
	}
	if len(s.stack) >= scriptDepthLimit {
		return fail("スクリプトの入れ子の深さが上限を超えます")
	}
	st, e := os.Stat(name)
	if e != nil || !st.Mode().IsRegular() {
		return fail("読み込み可能な通常ファイルではありません")
	}
	// O_NONBLOCK and fstat also protect the stat/open race against a FIFO swap.
	fd, e := unix.Open(name, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if e != nil {
		return fail("ファイルを開けません")
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	st, e = f.Stat()
	if e != nil || !st.Mode().IsRegular() {
		return fail("通常ファイルではありません")
	}
	for _, parent := range s.stack {
		if os.SameFile(st, parent) {
			return fail("スクリプトの自己再帰・循環を検出しました")
		}
	}
	if st.Size() > scriptFileLimit {
		return fail("1ファイルの読み込み上限を超えます")
	}
	data, e := io.ReadAll(io.LimitReader(f, scriptFileLimit+1))
	if e != nil || len(data) > scriptFileLimit {
		return fail("ファイルの読み込み失敗または上限超過です")
	}
	s.total += len(data)
	if s.total > scriptTotalLimit {
		return fail("スクリプト全体の読み込み上限を超えます")
	}
	s.stack = append(s.stack, st)
	defer func() { s.stack = s.stack[:len(s.stack)-1] }()
	return s.body(i, data, name)
}

func (s *scriptScanner) body(i Input, data []byte, name string) scriptResult {
	i.shellVariables = nil
	i.runtimeLine = false
	fail := func(reason string) scriptResult { return scriptIssue(scriptUnparseable, name+": "+reason) }
	tree, e := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(string(data)), name)
	if e != nil {
		return fail("スクリプト全体の構文を解析できません: " + e.Error())
	}
	i.Env = EnvMap(environment(i.Env))
	vars := map[string]bool{}
	for key, value := range i.scriptVariables {
		vars[key] = value
	}
	invalidate := func(name string) { vars[name] = true; delete(i.Env, name) }
	// A value assigned anywhere in the program is not the caller's value. This
	// deliberately includes function bodies and conditional branches.
	syntax.Walk(tree, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.Assign:
			if n.Name != nil {
				invalidate(n.Name.Value)
			}
		case *syntax.WordIter:
			invalidate(n.Name.Value)
		case *syntax.ArithmCmd, *syntax.ArithmExp:
			syntax.Walk(node, func(child syntax.Node) bool {
				if lit, ok := child.(*syntax.Lit); ok {
					invalidate(lit.Value)
				}
				return true
			})

		case *syntax.CallExpr:
			if len(n.Args) == 0 {
				break
			}
			var argv []string
			literalArgs := true
			for _, word := range n.Args {
				value, ok := claudeLiteral(word)
				literalArgs = literalArgs && ok
				argv = append(argv, value)
			}
			command, err := unwrapCommand(Input{Command: argv[0], Args: argv[1:], Env: EnvMap(environment(i.Env))})
			if err == nil && (command.Command == "read" || command.Command == "unset") {
				if !literalArgs {
					for name := range i.Env {
						invalidate(name)
					}
				}
				if command.Command == "read" {
					invalidate("REPLY")
				}
				for _, name := range command.Args {
					invalidate(name)
				}
			}

		}
		return true
	})
	if i.CwdUncertain || treeChangesDirectory(tree) {
		invalidate("PWD")
		invalidate("OLDPWD")
	}
	// Positional and special parameters belong to this script, never the parent.
	for key := range i.Env {
		if scriptSpecialParameter(key) {
			delete(i.Env, key)
		}
	}
	i.scriptVariables = vars
	i.scriptBody = true
	i.scriptLabel = name
	commands, e := lineCommands(i, string(data))
	best := scriptResult{}
	if v := classify(s.config, Input{RawLine: string(data)}); v.Decision == "block" {
		best = scriptResult{kind: scriptBlocked, verdict: v}
	}
	if e != nil {
		best = best.merge(scriptError(e))
	}
	// classify ignores temporary/day grants: a grant is not evidence of safety.
	for _, cmd := range commands {
		cmd.RawLine = ""
		var r scriptResult
		if cmd.scriptResult != nil {
			r = *cmd.scriptResult
		} else {
			v := classify(s.config, cmd)
			switch v.Decision {
			case "block":
				r = scriptResult{kind: scriptBlocked, verdict: v}
			case "ask":
				r = scriptResult{kind: scriptDanger, verdict: v}
				if v.Rule == "guard-boundary" && has([]string{"eval", "source", ".", "xargs", "sudo", "doas", "timeout", "stdbuf", "arch", "chroot", "setsid", "ionice", "find"}, filepath.Base(cmd.Command)) {
					r.kind = scriptUnparseable
				}
			}
			if cmd.Unresolved && r.kind != scriptBlocked {
				known := cmd
				known.Args = cmd.scriptKnownArgs
				if cmd.scriptUnknownCommand || classify(s.config, known).Decision != "ask" {
					r = scriptIssue(scriptUnresolved, "変数展開後のコマンドを確定できません")
				}
			}
		}
		if r.kind != scriptSafe && r.trigger == "" {
			lines := strings.Split(string(data), "\n")
			line := int(cmd.scriptLine)
			if line > 0 && line <= len(lines) {
				r.trigger = fmt.Sprintf("%s の%d行目: %s", name, line, lines[line-1])
				r.verdict.Reason = r.trigger + "\n" + r.verdict.Reason
			}
		}
		best = best.merge(r)
	}
	if best.kind == scriptUnresolved {
		if filepath.Base(i.Command) == "bash" && verifiedBashEnv(i) {
			return scriptResult{}
		}
		best = scriptIssue(scriptUnparseable, "変数で未確定のスクリプトに実行中の見張りを確認できません: "+name)
	}
	return best
}

func paneBashEnv(i Input) (string, bool) {
	m, active, e := ReadMarker(i)
	if e != nil || !active || m.Mode != "enforce" || !m.Policy.Active() || m.Policy.EffectiveMode() != "enforce" {
		return "", false
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(i.Env["IKESU_GUARD_MARKER"])))
	paneNumber, e := strconv.Atoi(strings.TrimPrefix(m.Pane, "p"))
	if e != nil || paneNumber <= 0 || m.Pane != "p"+strconv.Itoa(paneNumber) {
		return "", false
	}
	paneDir := "p_" + strconv.Itoa(paneNumber)
	a := NewAssets(root, paneDir, "", *m.Policy)
	a.meta = m
	if filepath.Clean(i.Env["IKESU_GUARD_MARKER"]) != a.MarkerPath {
		return "", false
	}
	exe, e := os.Executable()
	if e != nil {
		return "", false
	}
	actual, e := os.Stat(exe)
	if e != nil {
		return "", false
	}
	generatedExe := i.Env["IKESU_GUARD_EXECUTABLE"]
	generated, e := os.Stat(generatedExe)
	if e != nil || !os.SameFile(actual, generated) {
		return "", false
	}
	a.Executable = generatedExe
	dir := filepath.Join(root, "guard", "shell", paneDir)
	path := filepath.Join(dir, "bash-env")
	st, e := os.Lstat(path)
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0400 == 0 || st.Size() > scriptFileLimit {
		return "", false
	}
	data, e := os.ReadFile(path)
	if e != nil || string(data) != a.bashStartup(dir) {
		return "", false
	}
	return path, true
}
func verifiedBashEnv(i Input) bool {
	path, ok := paneBashEnv(i)
	return ok && i.Env["BASH_ENV"] == path && shellRuntimeArguments(i.Args) && knownBashExecutable(i)
}

// PrepareClaudeBashEnv pins the generated file before both preflight and exec;
// the generated startup chain still sources IKESU_GUARD_BASH_ENV_ORIG first.
func PrepareClaudeBashEnv(i Input) Input {
	i.Env = EnvMap(environment(i.Env))
	if path, ok := paneBashEnv(i); ok && i.Env["IKESU_GUARD_CLAUDE_PREFIX_ORIG"] == "" {
		i.claudeBashEnv = path
		i.Env["BASH_ENV"] = path
	}
	return i
}

// Reapply after Claude's snapshot has restored its exported environment. The
// user's zsh startup files and bootstrap structure keep their original order.
func ClaudeExecutionLine(i Input, line string) string {
	if i.claudeBashEnv == "" {
		return line
	}
	prefix := "export BASH_ENV=" + quote(i.claudeBashEnv) + "; "
	payload := ClaudePrefixLine(i, line)
	if payload == line {
		return prefix + line
	}
	tree, e := syntax.NewParser().Parse(strings.NewReader(line), "Claude execution")
	if e != nil {
		return line
	}
	syntax.Walk(tree, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) != 2 {
			return true
		}
		if name, ok := claudeLiteral(call.Args[0]); ok && name == "eval" {
			w := call.Args[1]
			line = line[:w.Pos().Offset()] + quote(prefix+payload) + line[w.End().Offset():]
			return false
		}
		return true
	})
	return line
}

func scriptWordUnknown(i Input, word *syntax.Word) bool {
	if !i.scriptBody {
		return false
	}
	unknown := false
	syntax.Walk(word, func(node syntax.Node) bool {
		if p, ok := node.(*syntax.ParamExp); ok && p.Param != nil {
			name := p.Param.Value
			if i.scriptVariables[name] || scriptSpecialParameter(name) {
				unknown = true
			}
		}
		return true
	})
	return unknown
}

func scriptSpecialParameter(name string) bool {
	if has([]string{"@", "*", "#", "?", "!", "-", "$"}, name) {
		return true
	}
	_, err := strconv.ParseUint(name, 10, 64)
	return err == nil
}

// The proof permits only the approved startup options before a file or -c.
// A command string's arguments are operands, not further startup options.
func shellRuntimeArguments(args []string) bool {
	for n := 0; n < len(args); n++ {
		a := args[n]
		if a == "-o" || a == "+o" {
			n++
			continue
		}
		if a == "--" || !strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "+") {
			break
		}
		if strings.HasPrefix(a, "--") || !strings.Contains(a, "c") {
			continue
		}
		for _, flag := range a[1:] {
			if !strings.ContainsRune("euxvc", flag) {
				return false
			}
		}
		_, _, prefixOK := shellFileArgument(args[:n])
		return prefixOK && n+1 < len(args)
	}
	_, file, optionsOnly := shellFileArgument(args)
	return file || optionsOnly
}

// OS-provided and standard Homebrew Cellar bash executables are trust anchors.
// Resolve the first
// executable in the command's actual PATH; never skip a same-named wrapper.
// Unrecognized installations fail closed instead of being run to identify them.
func knownBashExecutable(i Input) bool {
	command := i.Command
	if !strings.ContainsRune(command, '/') {
		found := false
		for _, dir := range filepath.SplitList(i.Env["PATH"]) {
			if !filepath.IsAbs(dir) {
				if i.CwdUncertain {
					return false
				}
				dir = filepath.Join(i.Cwd, dir)
			}
			candidate := filepath.Join(dir, command)
			st, e := os.Stat(candidate)
			if e == nil && st.Mode().IsRegular() && st.Mode()&0111 != 0 {
				command = candidate
				found = true
				break
			}
		}
		if !found {
			return false
		}
	} else if !filepath.IsAbs(command) {
		if i.CwdUncertain {
			return false
		}
		command = filepath.Join(i.Cwd, command)
	}
	actual, e := os.Stat(command)
	if e != nil || !actual.Mode().IsRegular() || actual.Mode()&0111 == 0 {
		return false
	}
	for _, anchor := range []string{"/bin/bash", "/usr/bin/bash"} {
		known, e := os.Stat(anchor)
		if e == nil && os.SameFile(actual, known) {
			return true
		}
	}
	real, e := filepath.EvalSymlinks(command)
	if e != nil || !homebrewBashPath(real) {
		return false
	}
	known, e := os.Stat(real)
	return e == nil && os.SameFile(actual, known)
}

func homebrewBashPath(real string) bool {
	if !filepath.IsAbs(real) {
		return false
	}
	for _, prefix := range []string{"/opt/homebrew", "/usr/local", "/home/linuxbrew/.linuxbrew"} {
		relative, e := filepath.Rel(filepath.Join(prefix, "Cellar", "bash"), real)
		if e != nil {
			continue
		}
		parts := strings.Split(relative, string(filepath.Separator))
		if len(parts) == 3 && parts[0] != "" && parts[0] != "." && parts[0] != ".." && parts[1] == "bin" && parts[2] == "bash" {
			return true
		}
	}
	return false
}
