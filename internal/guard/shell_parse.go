package guard

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

// Shell analysis never executes substitutions or invokes a shell. Script
// operands use the bounded file reader; Walk visits function bodies as well.
func lineCommands(i Input, line string) ([]Input, error) {
	tree, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(line), "guard line")
	if err != nil {
		if i.runtimeLine {
			return nil, nil
		}
		return nil, fmt.Errorf("シェルの構文を分解できません: %w", err)
	}
	i.GuardRoot = guardDirectory(i)
	originalCwd := i.Cwd
	cdCall := (*syntax.CallExpr)(nil)
	if newCwd, call, ok := literalCdChain(i, tree); ok && !i.CwdUncertain {
		i.Cwd, cdCall = newCwd, call
	} else {
		i.CwdUncertain = i.CwdUncertain || evalTreeChangesDirectory(i, tree)
	}
	cwdBeforeCd := i.Cwd
	if cdCall != nil {
		cwdBeforeCd = originalCwd
	}
	var commands []Input
	setError := func(e error) {
		if i.scriptBody {
			if err == nil || scriptError(e).kind > scriptError(err).kind {
				err = e
			}
		} else if err == nil || !unresolvedFailure(e) || unresolvedFailure(err) {
			err = e
		}
	}
	redirections := map[*syntax.CallExpr][]*syntax.Redirect{}
	syntax.Walk(tree, func(node syntax.Node) bool {
		if stmt, ok := node.(*syntax.Stmt); ok {
			if call, ok := stmt.Cmd.(*syntax.CallExpr); ok {
				redirections[call] = stmt.Redirs
			}
		}
		return true
	})
	boundaryInput := i
	boundaryInput.RawLine = line
	if e := treeBoundary(boundaryInput, tree); e != nil {
		setError(e)
	}
	syntax.Walk(tree, func(node syntax.Node) bool {
		if i.runtimeLine {
			switch node.(type) {
			case *syntax.CmdSubst, *syntax.ProcSubst, *syntax.FuncDecl:
				return false
			}
		}
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		next := i
		if call == cdCall {
			next.Cwd = cwdBeforeCd
		}
		next.scriptResult = nil
		callStart, callOffset := len(commands), int(call.Pos().Offset())
		// 入れ子の解析で増えた命令も、この呼び出しの位置の行に割り当てる。
		stamp := func() {
			for k := callStart; k < len(commands); k++ {
				commands[k].callOffset = callOffset
			}
		}
		next.callOffset = callOffset
		next.scriptLine = call.Pos().Line()
		next.scriptKnownArgs = nil
		next.Env = EnvMap(environment(i.Env))
		next.RawLine = line
		values := i.shellExpansionValues()
		cfg := &expand.Config{NoUnset: true}
		for _, assignment := range call.Assigns {
			if startupVariable(assignment.Name.Value) || assignment.Name.Value == "PATH" {
				next.ShellEnvChanged = true
				next.ClaudePrefixChanged = next.ClaudePrefixChanged || assignment.Name.Value == "CLAUDE_CODE_SHELL_PREFIX"
			}
			if assignment.Value == nil {
				continue
			}
			cfg.Env = expand.ListEnviron(environment(values)...)
			value, e := i.runtimeLiteral(cfg, assignment.Value)
			if e != nil {
				next.Unresolved = true
				delete(next.Env, assignment.Name.Value)
				continue
			}
			key := assignment.Name.Value
			values[key] = value
			if next.privateExpansion(assignment.Value, value) {
				next.probePrivate = true
				delete(next.Env, key)
			} else if i.scriptBody {
				delete(next.Env, key)
			} else if !authorizationIdentityKey(key) {
				next.Env[key] = value
			}
		}
		// Shell words expand in the parent environment, before prefix assignments.
		cfg.Env = expand.ListEnviron(environment(i.shellExpansionValues())...)
		var argv []string
		unknownCommand := false
		unknownWords := map[string]bool{}
		for n, word := range call.Args {
			value, e := i.runtimeLiteral(cfg, word)
			if scriptWordUnknown(i, word) {
				e = unresolvedScriptError{"スクリプト内で変わる変数です"}
			}
			if e != nil {
				next.Unresolved = true
				var printed bytes.Buffer
				_ = syntax.NewPrinter().Print(&printed, word)
				value = printed.String()
				unknownWords[value] = true
				if n == 0 {
					unknownCommand = true
				} else {
					next.UnresolvedArg = true
				}
			}
			if e == nil && i.privateExpansion(word, value) {
				next.probePrivate = true
			}
			if e == nil && n > 0 {
				next.scriptKnownArgs = append(next.scriptKnownArgs, value)
			}
			argv = append(argv, value)
		}
		next.Command, next.Args = argv[0], argv[1:]
		next.unknownWords = unknownWords
		if unknownCommand {
			setError(unresolvedScriptError{"実行するコマンド名を安全に確定できません: " + next.Command})
			next.scriptUnknownCommand = true
		}
		next.AuthorizationEnv = EnvMap(environment(i.AuthorizationEnv))
		for key, value := range next.Env {
			next.AuthorizationEnv[key] = value
		}
		unwrapped, e := unwrapCommand(next)
		if i.runtimeLine && unknownWords[unwrapped.Command] {
			setError(unresolvedScriptError{"実行するコマンド名を安全に確定できません"})
		}
		if next.probePrivate {
			unwrapped.Env = EnvMap(environment(next.Env))
		}
		unwrapped.scriptKnownArgs = nil
		for _, a := range unwrapped.Args {
			if has(next.scriptKnownArgs, a) {
				unwrapped.scriptKnownArgs = append(unwrapped.scriptKnownArgs, a)
			}
		}
		if !i.runtimeLine && filepath.Base(unwrapped.Command) == "eval" && len(call.Args)-1 == len(unwrapped.Args) && len(unwrapped.Args) > 0 && i.evalDepth < scriptDepthLimit {
			var words []string
			for _, word := range call.Args[1:] {
				value, ok := claudeLiteral(word)
				if !ok {
					words = nil
					break
				}
				words = append(words, value)
			}
			if len(words) > 0 {
				child := unwrapped
				child.evalDepth++
				nested, nestedErr := lineCommands(child, strings.Join(words, " "))
				commands = append(commands, nested...)
				stamp()
				if nestedErr != nil {
					setError(nestedErr)
				}
				return true
			}
		}
		commands = append(commands, unwrapped)
		if e != nil {
			setError(e)
		}
		if e := commandScript(unwrapped, redirections[call], &commands); e != nil {
			setError(e)
		}
		stamp()

		return true
	})
	return commands, err
}

func evalTreeChangesDirectory(i Input, tree *syntax.File) bool {
	if treeChangesDirectory(tree) {
		return true
	}
	return evalNestedChangesDirectory(i, tree)
}

// evalNestedChangesDirectory looks only for directory changes hidden in eval.
func evalNestedChangesDirectory(i Input, tree *syntax.File) bool {
	if i.runtimeLine || i.evalDepth >= scriptDepthLimit {
		return false
	}
	changed := false
	cfg := &expand.Config{NoUnset: true, Env: expand.ListEnviron(environment(i.shellExpansionValues())...)}
	syntax.Walk(tree, func(node syntax.Node) bool {
		if changed {
			return false
		}
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}
		command, err := i.runtimeLiteral(cfg, call.Args[0])
		if err != nil {
			return true
		}
		words := make([]string, 0, len(call.Args)-1)
		for _, word := range call.Args[1:] {
			value, literal := claudeLiteral(word)
			if !literal {
				return true
			}
			words = append(words, value)
		}
		unwrapped, err := unwrapCommand(Input{Command: command, Args: words, Env: EnvMap(environment(i.Env))})
		if err != nil || filepath.Base(unwrapped.Command) != "eval" || len(words) != len(unwrapped.Args) {
			return true
		}
		nested, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(strings.Join(words, " ")), "guard eval")
		if err == nil {
			child := i
			child.evalDepth++
			changed = evalTreeChangesDirectory(child, nested)
		}
		return true
	})
	return changed
}

func unwrapCommand(i Input) (Input, error) {
	for {
		cmd, args := filepath.Base(i.Command), i.Args
		take := func(n int) bool {
			if len(args) < n {
				return false
			}
			args = args[n:]
			return true
		}
		switch cmd {
		case "command":
			if len(args) > 0 && (args[0] == "-v" || args[0] == "-V") {
				return i, nil
			}
			if len(args) > 0 && args[0] == "-p" {
				args = args[1:]
			}
			if len(args) > 0 && args[0] == "--" {
				args = args[1:]
			}
		case "builtin", "nohup":
			if len(args) > 0 && args[0] == "--" {
				args = args[1:]
			}
		case "exec":
			for len(args) > 0 && strings.HasPrefix(args[0], "-") {
				option := args[0]
				args = args[1:]
				if option == "--" {
					break
				}
				if option == "-a" {
					if !take(1) {
						return i, prefixError(cmd)
					}
					continue
				}
				if option == "-c" {
					i.Env = map[string]string{}
					i.ShellEnvChanged = true
					i.ClaudePrefixChanged = true
				}
				if option != "-c" && option != "-l" {
					return i, prefixError(cmd)
				}
			}
		case "time":
			if len(args) > 0 && args[0] == "-p" {
				args = args[1:]
			}
			if len(args) > 0 && args[0] == "--" {
				args = args[1:]
			}
		case "nice":
			if len(args) > 0 && (args[0] == "-n" || args[0] == "--adjustment") {
				if !take(2) {
					return i, prefixError(cmd)
				}
			} else if len(args) > 0 && strings.HasPrefix(args[0], "--adjustment=") {
				args = args[1:]
			} else if len(args) > 0 && strings.HasPrefix(args[0], "-") && args[0] != "--" {
				if _, e := strconv.Atoi(args[0][1:]); e != nil {
					return i, prefixError(cmd)
				}
				args = args[1:]
			}
			if len(args) > 0 && args[0] == "--" {
				args = args[1:]
			}
		case "env":
			var e error
			args, e = envArguments(&i, args)
			if e != nil {
				return i, e
			}

		default:
			return i, nil
		}
		if len(args) == 0 {
			return i, nil
		}
		if strings.HasPrefix(args[0], "-") {
			return i, prefixError(cmd)
		}
		i.Command, i.Args = args[0], args[1:]
	}
}
func prefixError(cmd string) error {
	return fmt.Errorf("前置き %s の中のコマンドを確定できません", cmd)
}
func splitPrefix(text string, env map[string]string) ([]string, error) {
	var words []string
	var expandErr error
	err := syntax.NewParser().Words(strings.NewReader(text), func(word *syntax.Word) bool {
		value, e := shellLiteral(&expand.Config{Env: expand.ListEnviron(environment(env)...), NoUnset: true}, word)
		if e != nil {
			expandErr = e
			return false
		}
		words = append(words, value)
		return true
	})
	if err != nil || expandErr != nil || len(words) == 0 {
		return nil, fmt.Errorf("env -S の中のコマンドを分解できません")
	}
	return words, nil
}

// lineVerdict は判定に加えて、行ごとの判定の材料（judgement）を返す Input に付ける。
func lineVerdict(c Config, i Input, line string) (Input, Verdict, error) {
	j := &lineJudgement{line: line, blockOffset: -1}
	i, v, err := lineVerdictJudged(c, i, line, j)
	i.judgement = j
	return i, v, err
}

func lineVerdictJudged(c Config, i Input, line string, j *lineJudgement) (Input, Verdict, error) {
	i.GuardCommands = c.Commands()
	i.scriptScanner = &scriptScanner{config: c}
	// wide-scan は AI が打った最上位の行だけで判定する（DEBUG トラップの行・スクリプトの中身・シムは対象外）。
	i.wideScanLine = !i.runtimeLine
	commands, err := lineCommands(i, line)
	i.RawLine = line
	best := Evaluate(c, Input{Cwd: i.Cwd, LaunchCwd: i.LaunchCwd, Env: i.Env, RawLine: line, runtimeLine: i.runtimeLine})
	if blocksRun(best.Decision) {
		return i, best, nil
	}
	if !i.runtimeLine && danger(c, i) && i.Env["AI_GUARD_DANGER_WORD_ACK"] != "1" {
		best = result("ask", "danger-word-line", "deploy/publish を含む入力行は確認対象です")
	}
	var undetermined []string
	for _, command := range commands {
		v := result("allow", "default", "")
		if !(i.runtimeLine && has([]string{"eval", "source", "."}, filepath.Base(command.Command))) {
			v = Evaluate(c, command)
		}
		if v.Decision == "allow" && !i.runtimeLine {
			if uv, ok := unresolvedVerdict(c, command, Evaluate); ok {
				v = uv
			}
		}
		if command.scriptResult != nil {
			r := command.scriptResult
			if r.kind == scriptSafe {
				continue
			}
			if r.kind == scriptUnresolved {
				undetermined = append(undetermined, r.verdict.Reason)
				continue
			}
			v = r.verdict
			command.TriggerCommand = r.trigger
		}
		if v.Decision == "allow" && command.UnresolvedArg && has([]string{"rm", "rmdir", "rimraf", "trash"}, filepath.Base(command.Command)) {
			// 削除の対象が未確定な間は聞く（サンドボックスを入れるまでの例外）。
			v = result("ask", "rm-paths", "削除操作は確認が必要です")
		}
		j.add(command, v)
		if blocksRun(v.Decision) && !blocksRun(best.Decision) || v.Decision == "ask" && best.Decision == "allow" {
			i, best = command, v
		}
		if blocksRun(best.Decision) {
			j.blockOffset, j.blockSet = command.callOffset, true
			return i, best, nil
		}
	}
	if err != nil && unresolvedFailure(err) {
		undetermined = append(undetermined, err.Error())
		err = nil
	}
	if err != nil {
		if i.AuthorizationEnv == nil && len(commands) > 0 {
			i.AuthorizationEnv = commands[0].AuthorizationEnv
		}
		i.TriggerCommand = "入力行全体: " + line
		return i, result("ask", "shell-line", err.Error()), err
	}
	if best.Rule == "danger-word-line" {
		i.TriggerCommand = "入力行全体: " + line
		for _, command := range commands {
			command.RawLine = ""
			if danger(c, command) {
				j.add(command, best)
				i.TriggerCommand = command.Line()
				break
			}
		}
	}
	if best.Decision == "allow" && best.Rule == "default" && len(undetermined) > 0 {
		best.Reason = undeterminedPrefix + strings.Join(undetermined, " / ")
	}
	return i, best, nil
}

// 未確定を理由には聞かない。通したことは guard log の reason の印で区別する。
const undeterminedPrefix = "未確定を通した: "

func unresolvedFailure(e error) bool {
	var unresolved unresolvedScriptError
	return errors.As(e, &unresolved)
}

const unresolvedPlaceholder = "UNRESOLVED_VALUE"

// 未確定の語が混じる命令でも、外向きの規則・git の決まりは効かせる。
// (a) 未確定の語を「確定しない印の値」に置き換えた argv で判定する。
// (b) 先頭語が未確定なら、後ろのリテラルの語のうち既知のコマンド名から始まる部分列も判定する。
func unresolvedVerdict(c Config, command Input, judge func(Config, Input) Verdict) (Verdict, bool) {
	if !command.Unresolved {
		return Verdict{}, false
	}
	argv := append([]string{command.Command}, command.Args...)
	headUnknown := command.unknownWords[command.Command]
	for n, word := range argv {
		if command.unknownWords[word] {
			argv[n] = unresolvedPlaceholder
		}
	}
	try := func(words []string) (Verdict, bool) {
		candidate := command
		candidate.Unresolved, candidate.UnresolvedArg, candidate.unknownWords = false, false, nil
		candidate.RawLine = ""
		candidate.Command, candidate.Args = words[0], words[1:]
		v := judge(c, candidate)
		return v, blocksRun(v.Decision) || v.Decision == "ask"
	}
	var found Verdict
	have := false
	take := func(v Verdict, ok bool) {
		if ok && (!have || blocksRun(v.Decision) && !blocksRun(found.Decision)) {
			found, have = v, true
		}
	}
	if !headUnknown {
		take(try(argv))
	} else {
		for n := 1; n < len(argv); n++ {
			if argv[n] != unresolvedPlaceholder && has(c.Commands(), filepath.Base(argv[n])) {
				take(try(argv[n:]))
			}
		}
	}
	return found, have
}
