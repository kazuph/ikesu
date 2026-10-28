package guard

import (
	"fmt"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

func guardDirectory(i Input) string {
	if i.GuardRoot != "" {
		return i.GuardRoot
	}
	if p := i.Env["IKESU_GUARD_MARKER"]; p != "" {
		return filepath.Dir(filepath.Dir(p))
	}
	return ""
}
func guardFunction(i Input, name string) bool {
	return name == "TRAPDEBUG" || strings.HasPrefix(name, "_ikesu_guard_") || name == "command" || has(defaults(i.GuardCommands, (Config{}).Commands()), name)
}
func guardPath(i Input, path string) bool {
	root := guardDirectory(i)
	if root == "" || path == "" {
		return false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(i.Cwd, path)
	}
	for _, pair := range [][2]string{{filepath.Clean(root), filepath.Clean(path)}, {guardPhysical(root), guardPhysical(path)}} {
		if pair[1] == pair[0] || strings.HasPrefix(pair[1], pair[0]+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
func commandBoundary(i Input) string {
	if i.Command == "" {
		return ""
	}
	cmd, args := filepath.Base(i.Command), i.Args
	if i.Unresolved && has([]string{"trap", "unset", "unfunction", "functions", "alias", "unalias", "setopt", "unsetopt", "shopt", "set", "chmod", "chown", "chgrp"}, cmd) {
		return "判定・権限を変更する操作の対象を安全に確定できません"
	}
	switch cmd {
	case "ikesu", "herdr":
		if len(args) >= 2 && args[0] == "guard" && args[1] == "claude-prefix" {
			return "専用入口の実行内容は入口自身で全体を確認する必要があります"
		}
	case "eval", "source", ".", "xargs", "sudo", "doas", "timeout", "stdbuf", "arch", "chroot", "setsid", "ionice":
		return "実行を委譲する " + cmd + " の中身を安全に確定できません"
	case "find":
		for _, a := range args {
			if has([]string{"-exec", "-execdir", "-ok", "-okdir"}, a) {
				return "find が実行するコマンドの入力を確定できません"
			}
		}
	case "trap":
		if !has(args, "-p") {
			for _, a := range args {
				if has([]string{"DEBUG", "ERR", "RETURN"}, a) {
					return "判定のトラップの変更・解除には承認が必要です"
				}
			}
		}
	case "unset", "unsetenv", "setenv", "export", "typeset", "declare", "readonly", "unfunction", "functions", "alias", "unalias":
		for _, a := range args {
			name, _, _ := strings.Cut(a, "=")
			if guardFunction(i, name) || has([]string{"unset", "unsetenv", "setenv", "export", "typeset", "declare", "readonly"}, cmd) && startupVariable(name) {
				return "判定の関数・エイリアスの変更には承認が必要です"
			}
		}
	case "set":
		if setChangesGuard(args) {
			return "実行前の判定に関わる設定の変更には承認が必要です"
		}
	case "setopt", "unsetopt", "shopt":
		for _, a := range args {
			option := strings.TrimPrefix(strings.ToLower(strings.ReplaceAll(a, "_", "")), "no")
			if has([]string{"debugbeforecmd", "extdebug", "functrace", "errtrace", "localtraps", "localoptions"}, option) || has([]string{"+T", "-T", "+E", "-E"}, a) {
				return "実行前の判定に関わる設定の変更には承認が必要です"
			}
		}
	}
	var targets []string
	switch cmd {
	case "rm", "rmdir", "mv", "ln", "chmod", "chown", "chgrp", "tee", "touch", "truncate", "mkdir", "install", "trash", "rimraf":
		targets = args
	case "cp":
		if len(args) > 0 {
			targets = append(targets, args[len(args)-1])
		}
		for n, a := range args {
			if (a == "-t" || a == "--target-directory") && n+1 < len(args) {
				targets = append(targets, args[n+1])
			}
			if strings.HasPrefix(a, "--target-directory=") {
				targets = append(targets, strings.TrimPrefix(a, "--target-directory="))
			}
		}
	case "sed":
		for _, a := range args {
			if a == "--in-place" || strings.HasPrefix(a, "--in-place=") || strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.ContainsRune(a[1:], 'i') {
				targets = args
				break
			}
		}
	}
	for _, path := range targets {
		if !strings.HasPrefix(path, "-") && !filepath.IsAbs(path) && i.CwdUncertain {
			return "ディレクトリ変更を含む入力の書き込み先を安全に確定できません"
		}
		if !strings.HasPrefix(path, "-") && guardPath(i, path) {
			return "ガードの目印・シム・起動ファイルへの操作には承認が必要です"
		}
	}
	return ""
}

func treeBoundary(i Input, tree *syntax.File) error {
	var failure error
	cfg := &expand.Config{Env: expand.ListEnviron(environment(i.shellExpansionValues())...), NoUnset: true}
	syntax.Walk(tree, func(node syntax.Node) bool {
		if i.runtimeLine {
			switch node.(type) {
			case *syntax.CmdSubst, *syntax.ProcSubst, *syntax.FuncDecl:
				return false
			}
		}
		previous := failure
		defer func() {
			if i.scriptBody && previous != nil && (failure == nil || scriptError(previous).kind > scriptError(failure).kind) {
				failure = previous
			}
		}()

		if i.runtimeLine && reservedExpansionWrite(node) {
			failure = fmt.Errorf("右辺の展開による予約変数の変更には承認が必要です")
		}
		switch n := node.(type) {
		case *syntax.FuncDecl:
			if guardFunction(i, n.Name.Value) {
				failure = fmt.Errorf("判定と同名の関数の定義には承認が必要です")
			}
		case *syntax.DeclClause:
			removeExport, prefixName := false, false
			for _, assignment := range n.Args {
				if assignment.Value != nil {
					value, e := shellLiteral(cfg, assignment.Value)
					name, _, assigned := strings.Cut(value, "=")
					if e != nil && !i.runtimeLine || assigned && startupVariable(name) {
						failure = fmt.Errorf("子シェルの起動設定の変更内容を確認する必要があります")
					}
					removeExport = removeExport || value == "-n" || value == "+x"
					prefixName = prefixName || value == "CLAUDE_CODE_SHELL_PREFIX"
				}
				if assignment.Name != nil && startupVariable(assignment.Name.Value) {
					failure = fmt.Errorf("子シェルの起動ファイルを変更する設定には承認が必要です")
				}
			}
			if removeExport && prefixName {
				failure = fmt.Errorf("Claudeの実行前判定を子プロセスから消す設定には承認が必要です")
			}
		case *syntax.CallExpr:
			if len(n.Args) == 0 {
				for _, assignment := range n.Assigns {
					if assignment.Name != nil && startupVariable(assignment.Name.Value) {
						failure = fmt.Errorf("子シェルの起動ファイルを変更する単独代入には承認が必要です")
					}
				}
			}
		case *syntax.Redirect:
			switch n.Op {
			case syntax.RdrOut, syntax.AppOut, syntax.RdrInOut, syntax.RdrAll, syntax.AppAll, syntax.ClbOut, syntax.DplOut:
				path, e := i.runtimeLiteral(cfg, n.Word)
				if scriptWordUnknown(i, n.Word) {
					e = unresolvedScriptError{"スクリプト内で変わる変数です"}
				}
				if e != nil {
					if i.scriptBody {
						failure = unresolvedScriptError{"書き込み先のパスを安全に確定できません"}
					} else {
						failure = fmt.Errorf("書き込み先のパスを安全に確定できません")
					}
				} else if !i.runtimeLine && i.CwdUncertain && !filepath.IsAbs(path) && !(n.Op == syntax.DplOut && descriptorTarget(path)) {
					failure = fmt.Errorf("ディレクトリ変更を含む入力の書き込み先を安全に確定できません")
				} else if guardPath(i, path) {
					failure = fmt.Errorf("ガードの置き場所へのリダイレクトには承認が必要です")
				}
			}
		}
		return true
	})
	return failure
}

func commandScript(i Input, redirs []*syntax.Redirect, commands *[]Input) error {
	cmd := filepath.Base(i.Command)
	if i.ClaudePrefixChanged {
		return fmt.Errorf("Claudeの実行前判定を外す環境の変更には承認が必要です")
	}
	shell := has([]string{"sh", "bash", "zsh", "dash", "ksh"}, cmd)
	interpreter := shell || has([]string{"fish", "python", "python2", "python3", "perl", "ruby", "node", "osascript"}, cmd)
	if !interpreter {
		return nil
	}
	if i.ShellEnvChanged {
		return fmt.Errorf("子シェル・インタプリタの起動環境が変更され、中身を保証できません")
	}
	if shell {
		if i.OpaquePrefix {
			return fmt.Errorf("既存のClaude実行prefixが読み込むプログラムには承認が必要です")
		}
		path, isFile, optionsOnly := shellFileArgument(i.Args)
		if isFile {
			if i.scriptScanner == nil {
				i.scriptScanner = &scriptScanner{config: Config{}}
			}
			r := i.scriptScanner.file(i, path)
			i.scriptResult = &r
			*commands = append(*commands, i)
			return nil
		}
		// Only a command-string option before any operand retains the legacy -c
		// path. File arguments such as x.sh -c ls never enter it.
		commandString := false
		for n := 0; n < len(i.Args); n++ {
			a := i.Args[n]
			if a == "-o" || a == "+o" {
				n++
				continue
			}
			if !strings.HasPrefix(a, "-") || a == "--" {
				break
			}
			if !strings.HasPrefix(a, "--") && strings.Contains(a, "c") {
				commandString = true
				break
			}
		}
		if len(i.Args) > 0 && !commandString && !optionsOnly {
			return fmt.Errorf("シェルの起動引数・標準入力を確定できません")
		}
	}
	for n, option := range i.Args {
		if strings.HasPrefix(option, "-") && !strings.HasPrefix(option, "--") && strings.Contains(option, "c") && shell {
			if n+1 >= len(i.Args) {
				return prefixError(cmd)
			}
			if i.scriptBody && i.scriptScanner != nil {
				if !shellRuntimeArguments(i.Args) {
					return fmt.Errorf("入れ子のシェルの起動引数を確認する必要があります")
				}
				r := i.scriptScanner.body(i, []byte(i.Args[n+1]), i.scriptLabel)
				i.scriptResult = &r
				*commands = append(*commands, i)
				return nil
			}
			child := i
			child.shellVariables = nil
			child.shellArrays = nil
			child.runtimeLine = false
			nested, e := lineCommands(child, i.Args[n+1])
			*commands = append(*commands, nested...)
			return e
		}
	}
	if !shell && interpreterHasProgram(cmd, i.Args) {
		return nil
	}
	for _, redir := range redirs {
		switch redir.Op {
		case syntax.Hdoc, syntax.DashHdoc, syntax.WordHdoc:
			if !shell {
				return fmt.Errorf("インタプリタへ渡す標準入力のスクリプトを確定できません")
			}
			word := redir.Hdoc
			if redir.Op == syntax.WordHdoc {
				word = redir.Word
			}
			script, e := shellLiteral(&expand.Config{Env: expand.ListEnviron(environment(i.shellExpansionValues())...), NoUnset: true}, word)
			if e != nil {
				return fmt.Errorf("標準入力のスクリプトを安全に展開できません")
			}
			if i.scriptBody && i.scriptScanner != nil {
				r := i.scriptScanner.body(i, []byte(script), i.scriptLabel)
				i.scriptResult = &r
				*commands = append(*commands, i)
				return nil
			}
			child := i
			child.shellVariables = nil
			child.shellArrays = nil
			child.runtimeLine = false
			child.probePrivate = child.probePrivate || i.privateExpansion(word, script)
			nested, e := lineCommands(child, script)
			*commands = append(*commands, nested...)
			return e
		case syntax.RdrIn, syntax.RdrInOut, syntax.DplIn:
			return fmt.Errorf("ファイル・パイプから渡すスクリプトを確定できません")
		}
	}
	if !shell || len(i.Args) == 0 || has(i.Args, "-") {
		return fmt.Errorf("標準入力から実行するスクリプトを確定できません")
	}
	if shell {
		return fmt.Errorf("シェルが読み込むスクリプトの中身を確定できません")
	}
	return nil
}

// A redirect may create a new file below an existing symlinked directory.
func guardPhysical(path string) string {
	path = filepath.Clean(path)
	var suffix []string
	for {
		if real, e := filepath.EvalSymlinks(path); e == nil {
			for n := len(suffix) - 1; n >= 0; n-- {
				real = filepath.Join(real, suffix[n])
			}
			return real
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path
		}
		suffix = append(suffix, filepath.Base(path))
		path = parent
	}
}

func shellLiteral(cfg *expand.Config, word *syntax.Word) (string, error) {
	unsupported := false
	syntax.Walk(word, func(n syntax.Node) bool {
		if _, ok := n.(*syntax.ProcSubst); ok {
			unsupported = true
			return false
		}
		return true
	})
	if unsupported {
		return "", fmt.Errorf("プロセス置換は静的に展開できません")
	}
	return expand.Literal(cfg, word)
}

// This runtime-only check inspects expansion syntax without evaluating it.
func reservedExpansionWrite(node syntax.Node) bool {
	var target syntax.ArithmExpr
	switch n := node.(type) {
	case *syntax.ParamExp:
		return n.Exp != nil && (n.Exp.Op == syntax.AssignUnset || n.Exp.Op == syntax.AssignUnsetOrNull) && (snapshotReservedName(n.Param.Value) || startupVariable(n.Param.Value) || n.Param.Value == "PATH")
	case *syntax.BinaryArithm:
		switch n.Op {
		case syntax.Assgn, syntax.AddAssgn, syntax.SubAssgn, syntax.MulAssgn, syntax.QuoAssgn, syntax.RemAssgn, syntax.AndAssgn, syntax.OrAssgn, syntax.XorAssgn, syntax.ShlAssgn, syntax.ShrAssgn:
			target = n.X
		}
	case *syntax.UnaryArithm:
		if n.Op == syntax.Inc || n.Op == syntax.Dec {
			target = n.X
		}
	}
	if word, ok := target.(*syntax.Word); ok {
		name := word.Lit()
		return snapshotReservedName(name) || startupVariable(name) || name == "PATH"
	}
	return false
}
