package guard

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
func shellFileArgument(args []string) (string, bool) {
	for n := 0; n < len(args); n++ {
		a := args[n]
		if a == "--" {
			if n+1 < len(args) {
				return args[n+1], true
			}
			return "", false
		}
		if a == "-" {
			return "", false
		}
		if a == "-o" || a == "+o" {
			n++
			if n >= len(args) || !has([]string{"pipefail", "errexit", "nounset", "xtrace"}, args[n]) {
				return "", false
			}
			continue
		}
		if a == "--norc" || a == "--noprofile" {
			continue
		}
		if strings.HasPrefix(a, "-") || strings.HasPrefix(a, "+") {
			if !strings.HasPrefix(a, "-") || len(a) == 1 {
				return "", false
			}
			for _, c := range a[1:] {
				if !strings.ContainsRune("euxv", c) {
					return "", false
				}
			}
			continue
		}
		return a, true
	}
	return "", false
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
	fail := func(reason string) scriptResult { return scriptIssue(scriptUnparseable, name+": "+reason) }
	tree, e := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(string(data)), name)
	if e != nil {
		return fail("スクリプト全体の構文を解析できません: " + e.Error())
	}
	i.Env = EnvMap(environment(i.Env))
	// A value assigned anywhere in the program is not the caller's value. This
	// deliberately includes function bodies and conditional branches.
	syntax.Walk(tree, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.Assign:
			if n.Name != nil {
				delete(i.Env, n.Name.Value)
			}
		case *syntax.WordIter:
			delete(i.Env, n.Name.Value)
		case *syntax.CallExpr:
			if len(n.Args) > 0 {
				if cmd, ok := claudeLiteral(n.Args[0]); ok && (cmd == "read" || cmd == "unset") {
					for _, w := range n.Args[1:] {
						if v, ok := claudeLiteral(w); ok {
							delete(i.Env, v)
						}
					}
				}
			}
		}
		return true
	})
	// Positional and special parameters belong to this script, never the parent.
	for key := range i.Env {
		if strings.ContainsAny(key, "0123456789@*#?!-$") {
			delete(i.Env, key)
		}
	}
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
	a := NewAssets(root, m.Pane, "", *m.Policy)
	a.meta = m
	if filepath.Clean(i.Env["IKESU_GUARD_MARKER"]) != a.MarkerPath {
		return "", false
	}
	exe, e := os.Executable()
	if e != nil {
		return "", false
	}
	a.Executable = exe
	dir := filepath.Join(root, "guard", "shell", m.Pane)
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
	return ok && i.Env["BASH_ENV"] == path
}

// PrepareClaudeBashEnv pins the generated file before both preflight and exec;
// the generated startup chain still sources IKESU_GUARD_BASH_ENV_ORIG first.
func PrepareClaudeBashEnv(i Input) Input {
	i.Env = EnvMap(environment(i.Env))
	if path, ok := paneBashEnv(i); ok {
		i.Env["BASH_ENV"] = path
	}
	return i
}
