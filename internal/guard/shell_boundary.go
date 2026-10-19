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
	if p := i.Env["GTX_GUARD_MARKER"]; p != "" {
		return filepath.Dir(filepath.Dir(p))
	}
	return ""
}
func guardFunction(i Input, name string) bool {
	return name == "TRAPDEBUG" || strings.HasPrefix(name, "_gtx_guard_") || name == "command" || has(defaults(i.GuardCommands, (Config{}).Commands()), name)
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
	switch cmd {
	case "eval", "source", ".", "xargs", "sudo", "doas":
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
	case "unset", "unfunction", "functions", "alias", "unalias":
		for _, a := range args {
			name, _, _ := strings.Cut(a, "=")
			if guardFunction(i, name) || cmd == "unset" && startupVariable(name) {
				return "判定の関数・エイリアスの変更には承認が必要です"
			}
		}
	case "setopt", "unsetopt", "shopt", "set":
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
			if a == "--in-place" || strings.HasPrefix(a, "--in-place=") || strings.HasPrefix(a, "-i") {
				targets = args
				break
			}
		}
	}
	for _, path := range targets {
		if !strings.HasPrefix(path, "-") && guardPath(i, path) {
			return "ガードの目印・シム・起動ファイルへの操作には承認が必要です"
		}
	}
	return ""
}

func treeBoundary(i Input, tree *syntax.File) error {
	var failure error
	cfg := &expand.Config{Env: expand.ListEnviron(environment(i.Env)...), NoUnset: true}
	syntax.Walk(tree, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.FuncDecl:
			if guardFunction(i, n.Name.Value) {
				failure = fmt.Errorf("判定と同名の関数の定義には承認が必要です")
			}
		case *syntax.DeclClause:
			for _, assignment := range n.Args {
				if assignment.Name != nil && startupVariable(assignment.Name.Value) && assignment.Value != nil {
					failure = fmt.Errorf("子シェルの起動ファイルを変更する設定には承認が必要です")
				}
			}
		case *syntax.Redirect:
			switch n.Op {
			case syntax.RdrOut, syntax.AppOut, syntax.RdrInOut, syntax.RdrAll, syntax.AppAll:
				path, e := expand.Literal(cfg, n.Word)
				if e != nil {
					failure = fmt.Errorf("書き込み先のパスを安全に確定できません")
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
	shell := has([]string{"sh", "bash", "zsh", "dash", "ksh"}, cmd)
	interpreter := shell || has([]string{"fish", "python", "python2", "python3", "perl", "ruby", "node"}, cmd)
	if !interpreter {
		return nil
	}
	if i.ShellEnvChanged {
		return fmt.Errorf("子シェル・インタプリタの起動環境が変更され、中身を保証できません")
	}
	for n, option := range i.Args {
		if strings.HasPrefix(option, "-") && !strings.HasPrefix(option, "--") && strings.Contains(option, "c") && shell {
			if n+1 >= len(i.Args) {
				return prefixError(cmd)
			}
			nested, e := lineCommands(i, i.Args[n+1])
			*commands = append(*commands, nested...)
			return e
		}
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
			script, e := expand.Literal(&expand.Config{Env: expand.ListEnviron(environment(i.Env)...), NoUnset: true}, word)
			if e != nil {
				return fmt.Errorf("標準入力のスクリプトを安全に展開できません")
			}
			nested, e := lineCommands(i, script)
			*commands = append(*commands, nested...)
			return e
		case syntax.RdrIn, syntax.RdrInOut, syntax.DplIn:
			return fmt.Errorf("ファイル・パイプから渡すスクリプトを確定できません")
		}
	}
	if len(i.Args) == 0 || has(i.Args, "-") {
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
