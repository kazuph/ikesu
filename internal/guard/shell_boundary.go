package guard

import (
	"bytes"
	"fmt"
	"os"
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
	// The fixed entry is a symlink to the server's native executable. Protect
	// writes to that real target too, including replacement via its parent.
	entry := filepath.Join(root, "shell", strings.TrimSuffix(filepath.Base(i.Env["IKESU_GUARD_MARKER"]), ".json"), "ikesu")
	if target, err := filepath.EvalSymlinks(entry); err == nil {
		physical := guardPhysical(path)
		if physical == target || strings.HasPrefix(target, physical+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// boundaryTier は guard 自身の境界に関わる確認の理由とティアを返す（理由が空なら確認なし）。
func boundaryTier(i Input) (string, int) {
	if i.Command == "" {
		return "", 0
	}
	cmd, args := filepath.Base(i.Command), i.Args
	if i.Unresolved && has([]string{"trap", "unset", "unfunction", "functions", "alias", "unalias", "setopt", "unsetopt", "shopt", "set", "chmod", "chown", "chgrp"}, cmd) {
		const reason = "判定・権限を変更する操作の対象を安全に確定できません"
		switch cmd {
		case "set":
			if arg(args, 0) == "--" {
				return reason, TierL1
			}
		case "chmod", "chown", "chgrp":
			return reason, relativeWriteTier(i, cmd, args)
		}
		return reason, TierL3
	}
	switch cmd {
	case "env":
		if reason := envDropsGuard(i, args); reason != "" {
			return reason, TierL3
		}
	case "ikesu", "herdr":
		if len(args) >= 2 && args[0] == "guard" && args[1] == "claude-prefix" {
			return "専用入口の実行内容は入口自身で全体を確認する必要があります", TierL3
		}
	case "eval", "source", ".", "xargs", "sudo", "doas", "timeout", "stdbuf", "arch", "chroot", "setsid", "ionice":
		return "実行を委譲する " + cmd + " の中身を安全に確定できません", TierL3
	case "find":
		for _, a := range args {
			if has([]string{"-exec", "-execdir", "-ok", "-okdir"}, a) {
				return "find が実行するコマンドの入力を確定できません", TierL3
			}
		}
	case "trap":
		if !has(args, "-p") {
			for _, a := range args {
				if has([]string{"DEBUG", "ERR", "RETURN"}, a) {
					return "判定のトラップの変更・解除には承認が必要です", TierL3
				}
			}
		}
	case "unset", "unsetenv", "setenv", "export", "typeset", "declare", "readonly", "unfunction", "functions", "alias", "unalias":
		for _, a := range args {
			name, _, _ := strings.Cut(a, "=")
			if guardFunction(i, name) || has([]string{"unset", "unsetenv", "setenv", "export", "typeset", "declare", "readonly"}, cmd) && startupVariable(name) {
				return "判定の関数・エイリアスの変更には承認が必要です", TierL3
			}
		}
	case "set":
		if setChangesGuard(args) {
			return "実行前の判定に関わる設定の変更には承認が必要です", TierL3
		}
	case "setopt", "unsetopt", "shopt":
		// unsetopt and shopt -u clear options; setopt and shopt -s set them.
		off := cmd == "unsetopt"
		for _, a := range args {
			switch a {
			case "-u":
				off = true
				continue
			case "-s":
				off = false
				continue
			case "+T":
				return "実行前の判定に関わる設定の変更には承認が必要です", TierL3
			case "-T", "-E", "+E":
				continue
			}
			if optionWeakensGuard(a, off) {
				return "実行前の判定に関わる設定の変更には承認が必要です", TierL3
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
	reason, tier := "", 0
	for _, path := range targets {
		if !strings.HasPrefix(path, "-") && !filepath.IsAbs(path) && i.CwdUncertain && reason == "" {
			reason, tier = "ディレクトリ変更を含む入力の書き込み先を安全に確定できません", relativeWriteTier(i, cmd, targets)
		}
		if !strings.HasPrefix(path, "-") && guardPath(i, path) {
			return "ガードの目印・シム・起動ファイルへの操作には承認が必要です", TierL3
		}
	}
	return reason, tier
}

// relativeWriteTier は、cd の後の相対パスへ書く操作のティア。削除は L3。それ以外は
// 行や引数に guard の置き場所の手がかりが見えなければ L1。
func relativeWriteTier(i Input, cmd string, targets []string) int {
	if has([]string{"rm", "rmdir", "trash", "rimraf"}, cmd) {
		return TierL3
	}
	if root := guardDirectory(i); root != "" && strings.Contains(i.Line(), root) {
		return TierL3
	}
	for _, path := range targets {
		if !strings.HasPrefix(path, "-") && guardNameClue(i, path) {
			return TierL3
		}
	}
	return TierL1
}

func treeBoundary(i Input, tree *syntax.File) error {
	var failure error
	cfg := &expand.Config{Env: expand.ListEnviron(environment(i.shellExpansionValues())...), NoUnset: true}
	// L1 の失敗は、すでに見つけた L2 以上の失敗を隠さない。
	lowFailure := func(e error) {
		if failure == nil || unresolvedFailure(failure) || errTier(failure) <= TierL1 {
			failure = e
		}
	}
	syntax.Walk(tree, func(node syntax.Node) bool {
		if p, ok := node.(*syntax.ParamExp); ok && (unsupportedParamOp(p) || unquotableParamValue(cfg, p)) {
			failure = fmt.Errorf("未対応の変数展開には承認が必要です")
			return false
		}
		if _, ok := node.(*syntax.ProcSubst); ok {
			lowFailure(lowTierErrorf("プロセス置換が実行する内容には承認が必要です"))
			return false
		}
		if _, ok := node.(*syntax.ExtGlob); ok {
			lowFailure(lowTierErrorf("拡張グロブの展開には承認が必要です"))
			return false
		}
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

		if i.runtimeLine {
			changed, display := runtimeReasonSyntax(i, node, cfg)
			if display {
				return false
			}
			if changed {
				failure = fmt.Errorf("申請文の名前を含む操作・動的な変数への書込みには承認が必要です")
			}
		}
		if i.runtimeLine && reservedExpansionWrite(i, node) {
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
						// 宣言の名前が決まっていて予約名でなければ L1。名前が動的・予約名は L3。
						if assignment.Name != nil && !reservedDeclName(assignment.Name.Value) && !(assigned && startupVariable(name)) {
							lowFailure(lowTierErrorf("子シェルの起動設定の変更内容を確認する必要があります"))
						} else {
							failure = fmt.Errorf("子シェルの起動設定の変更内容を確認する必要があります")
						}
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
					// 未確定なだけでは聞かない。確定済みの失敗は上書きしない。
					// ただしガード自身の変数を使う書き込み先は、見えている時点で確認する。
					if strings.Contains(wordText(n.Word), "IKESU_GUARD") || guardNameClue(i, wordText(n.Word)) {
						failure = fmt.Errorf("ガードの置き場所を指す変数で書き込み先を組み立てる操作には承認が必要です")
					} else if failure == nil || unresolvedFailure(failure) {
						failure = unresolvedScriptError{"書き込み先のパスを安全に確定できません"}
					}
				} else if !i.runtimeLine && i.CwdUncertain && !filepath.IsAbs(path) && !(n.Op == syntax.DplOut && descriptorTarget(path)) {
					if root := guardDirectory(i); root != "" && (strings.Contains(i.RawLine, root) || guardNameClue(i, wordText(n.Word))) {
						// 行にガードの置き場所が見えている時は、cd 後の相対パスでも確認する。
						failure = fmt.Errorf("ディレクトリ変更を含む入力の書き込み先を安全に確定できません")
					} else if failure == nil || unresolvedFailure(failure) {
						failure = unresolvedScriptError{"ディレクトリ変更を含む入力の書き込み先を安全に確定できません"}
					}
				} else if guardPath(i, path) {
					failure = fmt.Errorf("ガードの置き場所へのリダイレクトには承認が必要です")
				}
			}
		}
		return true
	})
	return failure
}

func reservedDeclName(name string) bool {
	return startupVariable(name) || name == "PATH" || strings.HasPrefix(name, "IKESU_") || strings.HasPrefix(name, "AI_GUARD_")
}

// シェルへ渡すスクリプトを確定できない時のティア。bash が検証済みの起動環境で
// 動いているなら L1、それ以外のシェルは L3。シェル以外のインタプリタは L1。
func scriptInputTier(i Input, shell bool) int {
	if !shell || filepath.Base(i.Command) == "bash" && verifiedBashEnv(i) {
		return TierL1
	}
	return TierL3
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
				return lowTierErrorf("インタプリタへ渡す標準入力のスクリプトを確定できません")
			}
			word := redir.Hdoc
			if redir.Op == syntax.WordHdoc {
				word = redir.Word
			}
			script, e := shellLiteral(&expand.Config{Env: expand.ListEnviron(environment(i.shellExpansionValues())...), NoUnset: true}, word)
			if e != nil {
				return tierError(fmt.Errorf("標準入力のスクリプトを安全に展開できません"), scriptInputTier(i, true))
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
			return tierError(fmt.Errorf("ファイル・パイプから渡すスクリプトを確定できません"), scriptInputTier(i, shell))
		}
	}
	if !shell || len(i.Args) == 0 || has(i.Args, "-") {
		return tierError(fmt.Errorf("標準入力から実行するスクリプトを確定できません"), scriptInputTier(i, shell))
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

func unsupportedParamOp(p *syntax.ParamExp) bool {
	if p.Exp == nil || p.Exp.Op != syntax.OtherParamOps {
		return false
	}
	parts := p.Exp.Word.Parts
	if len(parts) != 1 {
		return true
	}
	lit, ok := parts[0].(*syntax.Lit)
	return !ok || (lit.Value != "Q" && lit.Value != "E")
}

func unquotableParamValue(cfg *expand.Config, p *syntax.ParamExp) bool {
	if cfg == nil || cfg.Env == nil || p.Exp == nil || p.Exp.Op != syntax.OtherParamOps || p.Exp.Word.Lit() != "Q" {
		return false
	}
	// Quote rejects NUL. Check all values because indirect expansions and array
	// indexes can select a different variable from the one named in the AST.
	invalid := false
	cfg.Env.Each(func(name string, v expand.Variable) bool {
		invalid = strings.ContainsRune(name, 0) || strings.ContainsRune(v.Str, 0)
		for _, value := range v.List {
			invalid = invalid || strings.ContainsRune(value, 0)
		}
		for key, value := range v.Map {
			invalid = invalid || strings.ContainsRune(key, 0) || strings.ContainsRune(value, 0)
		}
		return !invalid
	})
	return invalid
}

func shellLiteral(cfg *expand.Config, word *syntax.Word) (string, error) {
	if word == nil {
		return "", fmt.Errorf("展開する単語がありません")
	}
	var unsupported error
	syntax.Walk(word, func(n syntax.Node) bool {
		switch p := n.(type) {
		case *syntax.ParamExp:
			if unsupportedParamOp(p) || unquotableParamValue(cfg, p) {
				unsupported = fmt.Errorf("未対応の変数展開は静的に展開できません")
				return false
			}
		case *syntax.ProcSubst:
			unsupported = fmt.Errorf("プロセス置換は静的に展開できません")
			return false
		case *syntax.ExtGlob:
			unsupported = fmt.Errorf("拡張グロブは静的に展開できません")
			return false
		}
		return true
	})
	if unsupported != nil {
		return "", unsupported
	}
	return expand.Literal(cfg, word)
}

// This runtime-only check inspects expansion syntax without evaluating it.
func reservedExpansionWrite(i Input, node syntax.Node) bool {
	var target syntax.ArithmExpr
	switch n := node.(type) {
	case *syntax.ParamExp:
		return n.Exp != nil && (n.Exp.Op == syntax.AssignUnset || n.Exp.Op == syntax.AssignUnsetOrNull) && (runtimeReasonVariable(i, n.Param.Value) || snapshotReservedName(n.Param.Value) || startupVariable(n.Param.Value) || n.Param.Value == "PATH")
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
		return runtimeReasonVariable(i, name) || snapshotReservedName(name) || startupVariable(name) || name == "PATH"
	}
	return false
}

func runtimeReasonVariable(i Input, name string) bool {
	return has([]string{"AI_GUARD_REQUEST_REASON", "AI_GUARD_REQUEST_ENV_NAME", "IKESU_GUARD_PARENT_REASON", requestReasonEnvName(i)}, name)
}

func wordText(word *syntax.Word) string {
	var printed bytes.Buffer
	_ = syntax.NewPrinter().Print(&printed, word)
	return printed.String()
}

// 未確定な書き込み先でも、リテラルの名前がガードの保護対象（置き場所の名前・
// その直下の名前・目印のファイル名）に当たる時は確認を残す。名前の手がかりが無い
// 書き込み先（"$X" など）は対象外。
func guardNameClue(i Input, text string) bool {
	root := guardDirectory(i)
	if root == "" {
		return false
	}
	names := map[string]bool{filepath.Base(root): true, "bash-env": true}
	if marker := i.Env["IKESU_GUARD_MARKER"]; marker != "" {
		names[filepath.Base(marker)] = true
	}
	if entries, err := os.ReadDir(root); err == nil {
		for _, e := range entries {
			names[e.Name()] = true
		}
	}
	segments := strings.FieldsFunc(text, func(r rune) bool { return strings.ContainsRune("/$\"'`{}()<>;&| \t\n=:", r) })
	for _, s := range segments {
		if names[s] {
			return true
		}
	}
	return false
}
