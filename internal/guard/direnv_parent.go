package guard

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

// direnv evaluates .envrc by starting its own bash, which inherits this guard.
// Only that bash's fixed bootstrap lines are trusted; the .envrc commands
// themselves are still judged one by one.

func homebrewDirenvPath(real string) bool {
	if !filepath.IsAbs(real) {
		return false
	}
	for _, prefix := range []string{"/opt/homebrew", "/usr/local", "/home/linuxbrew/.linuxbrew"} {
		relative, e := filepath.Rel(filepath.Join(prefix, "Cellar", "direnv"), real)
		if e != nil {
			continue
		}
		parts := strings.Split(relative, string(filepath.Separator))
		if len(parts) == 3 && parts[0] != "" && parts[0] != "." && parts[0] != ".." && parts[1] == "bin" && parts[2] == "direnv" {
			return true
		}
	}
	return false
}

// ShellParentDirenv returns the real paths of the Homebrew direnv that started
// the bash running this guard and of that bash, or "" when that cannot be
// proven. The bash is
// shellPID (Bash's $$) or, when empty, this process's direct parent. Identity
// comes from the OS process table (pid plus start time), never from the
// command text, PATH or hash tables.
func ShellParentDirenv(shellPID string) (string, string) {
	self := os.Getpid()
	shell := 0
	if shellPID == "" {
		parent, _, err := approvalProcess(self)
		if err != nil {
			return "", ""
		}
		shell = parent
	} else {
		n, err := strconv.Atoi(shellPID)
		if err != nil || n <= 1 {
			return "", ""
		}
		shell = n
		found := false
		for pid, hop := self, 0; hop < 4; hop++ {
			parent, _, err := approvalProcess(pid)
			if err != nil || parent <= 1 {
				return "", ""
			}
			if parent == shell {
				found = true
				break
			}
			pid = parent
		}
		if !found {
			return "", ""
		}
	}
	if shell <= 1 {
		return "", ""
	}
	direnv, shellStart, err := approvalProcess(shell)
	if err != nil || direnv <= 1 || shellStart == "" {
		return "", ""
	}
	shellExe, err := approvalExecutable(shell)
	if err != nil || filepath.Base(shellExe) != "bash" {
		return "", ""
	}
	shellReal, err := filepath.EvalSymlinks(shellExe)
	if err != nil {
		return "", ""
	}
	_, direnvStart, err := approvalProcess(direnv)
	if err != nil || direnvStart == "" {
		return "", ""
	}
	exe, err := approvalExecutable(direnv)
	if err != nil || !filepath.IsAbs(exe) {
		return "", ""
	}
	real, err := filepath.EvalSymlinks(exe)
	if err != nil || !homebrewDirenvPath(real) {
		return "", ""
	}
	// Guard against pid reuse between the reads above.
	if again, start, err := approvalProcess(shell); err != nil || again != direnv || start != shellStart {
		return "", ""
	}
	if _, start, err := approvalProcess(direnv); err != nil || start != direnvStart {
		return "", ""
	}
	return real, shellReal
}

func literalPathWord(word *syntax.Word) (string, bool) {
	literal := true
	syntax.Walk(word, func(n syntax.Node) bool {
		switch n.(type) {
		case nil, *syntax.Word, *syntax.Lit, *syntax.SglQuoted, *syntax.DblQuoted:
		default:
			literal = false
		}
		return literal
	})
	if !literal {
		return "", false
	}
	value, err := shellLiteral(&expand.Config{NoUnset: true}, word)
	if err != nil || !filepath.IsAbs(value) || strings.ContainsRune(value, 0) {
		return "", false
	}
	return value, true
}

func plainCall(stmt *syntax.Stmt) (*syntax.CallExpr, bool) {
	if stmt == nil || stmt.Negated || stmt.Background || stmt.Coprocess || len(stmt.Redirs) != 0 {
		return nil, false
	}
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(call.Assigns) != 0 {
		return nil, false
	}
	return call, true
}

func wordLit(word *syntax.Word) string {
	if word == nil || len(word.Parts) != 1 {
		return ""
	}
	if lit, ok := word.Parts[0].(*syntax.Lit); ok {
		return lit.Value
	}
	return ""
}

// direnvStdlibEval matches: eval "$("<direnv>" stdlib)"
func direnvStdlibEval(call *syntax.CallExpr, direnv string) bool {
	if call == nil || len(call.Args) != 2 || wordLit(call.Args[0]) != "eval" {
		return false
	}
	quoted := call.Args[1]
	if len(quoted.Parts) != 1 {
		return false
	}
	dq, ok := quoted.Parts[0].(*syntax.DblQuoted)
	if !ok || dq.Dollar || len(dq.Parts) != 1 {
		return false
	}
	sub, ok := dq.Parts[0].(*syntax.CmdSubst)
	if !ok || sub.Backquotes || sub.TempFile || sub.ReplyVar || len(sub.Stmts) != 1 || len(sub.Last) != 0 {
		return false
	}
	inner, ok := plainCall(sub.Stmts[0])
	if !ok || len(inner.Args) != 2 || wordLit(inner.Args[1]) != "stdlib" {
		return false
	}
	path, ok := literalPathWord(inner.Args[0])
	if !ok {
		return false
	}
	real, err := filepath.EvalSymlinks(path)
	return err == nil && real == direnv
}

// direnvBootstrapLine reports whether line is exactly what direnv asks its bash
// to run: eval "$(<direnv> stdlib)" && __main__ source_env <literal path>
// (or the first half alone, which Bash's DEBUG trap reports separately).
func direnvBootstrapLine(line, direnv string) bool {
	if direnv == "" || strings.ContainsAny(line, "\n\r") {
		return false
	}
	tree, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(line), "")
	if err != nil || len(tree.Stmts) != 1 || len(tree.Last) != 0 {
		return false
	}
	stmt := tree.Stmts[0]
	if stmt.Negated || stmt.Background || stmt.Coprocess || len(stmt.Redirs) != 0 {
		return false
	}
	switch cmd := stmt.Cmd.(type) {
	case *syntax.CallExpr:
		call, ok := plainCall(stmt)
		return ok && direnvStdlibEval(call, direnv)
	case *syntax.BinaryCmd:
		if cmd.Op != syntax.AndStmt {
			return false
		}
		left, ok := plainCall(cmd.X)
		if !ok || !direnvStdlibEval(left, direnv) {
			return false
		}
		right, ok := plainCall(cmd.Y)
		if !ok || len(right.Args) != 3 || wordLit(right.Args[0]) != "__main__" || wordLit(right.Args[1]) != "source_env" {
			return false
		}
		_, ok = literalPathWord(right.Args[2])
		return ok
	}
	return false
}

// direnvMainFrames is the function stack seen by the DEBUG hook for the
// "$@" line at the end of direnv's stdlib __main__. Bash reports the same
// line once more on entry to source_env, so that stack is accepted too. A
// "$@" written inside an .envrc always sits above an extra source frame.
//
// The names alone can be imitated: an .envrc may define its own __main__ and
// call it from an EXIT trap, which yields the same stack. BASH_SOURCE tells
// them apart: functions from direnv's stdlib are defined by the eval in the
// bash -c string, so their source is empty (bash 3.2) or the bash executable
// itself ($0, bash 5), while a function defined in an .envrc names that file.
func direnvMainFrames(arrays map[string]map[int]string, shell string) bool {
	frames := arrays["FUNCNAME"]
	sources := arrays["BASH_SOURCE"]
	for _, want := range [][]string{
		{"_ikesu_guard_debug", "_ikesu_guard_dispatch", "__main__"},
		{"_ikesu_guard_debug", "_ikesu_guard_dispatch", "source_env", "__main__"},
	} {
		if len(frames) != len(want) {
			continue
		}
		match := true
		for index, name := range want {
			if frames[index] != name {
				match = false
			}
			if index >= 2 && !stdlibSource(sources[index], shell) {
				match = false
			}
		}
		if match {
			return true
		}
	}
	return false
}

// stdlibSource reports whether a BASH_SOURCE entry belongs to code defined by
// the bash -c string: empty, or a path to the running bash executable (a
// binary cannot be sourced, so an .envrc cannot produce it).
func stdlibSource(source, shell string) bool {
	if source == "" {
		return true
	}
	if shell == "" || !filepath.IsAbs(source) {
		return false
	}
	real, err := filepath.EvalSymlinks(source)
	return err == nil && real == shell
}

// trustedDirenvLine decides whether this exact line is direnv's own bootstrap
// in a bash that direnv started.
func trustedDirenvLine(i Input, line string) bool {
	if i.ShellParentDirenv == nil {
		return false
	}
	trimmed := strings.TrimSpace(line)
	bootstrap := strings.HasPrefix(trimmed, "eval ")
	main := trimmed == `"$@"` && i.shellArrays != nil
	if !bootstrap && !main {
		return false
	}
	direnv, shell := i.ShellParentDirenv()
	if direnv == "" {
		return false
	}
	if main {
		return direnvMainFrames(i.shellArrays, shell)
	}
	return direnvBootstrapLine(trimmed, direnv)
}
