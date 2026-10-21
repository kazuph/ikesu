package guard

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

// Shell analysis never evaluates substitutions, reads files, or invokes a
// shell. Walk also visits commands in substitutions and function bodies.
func lineCommands(i Input, line string) ([]Input, error) {
	tree, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(line), "guard line")
	if err != nil {
		return nil, fmt.Errorf("シェルの構文を分解できません: %w", err)
	}
	i.GuardRoot = guardDirectory(i)
	i.CwdUncertain = i.CwdUncertain || treeChangesDirectory(tree)
	var commands []Input
	redirections := map[*syntax.CallExpr][]*syntax.Redirect{}
	syntax.Walk(tree, func(node syntax.Node) bool {
		if stmt, ok := node.(*syntax.Stmt); ok {
			if call, ok := stmt.Cmd.(*syntax.CallExpr); ok {
				redirections[call] = stmt.Redirs
			}
		}
		return true
	})
	if e := treeBoundary(i, tree); e != nil {
		err = e
	}
	syntax.Walk(tree, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		next := i
		next.Env = EnvMap(environment(i.Env))
		next.RawLine = line
		cfg := &expand.Config{Env: expand.ListEnviron(environment(next.Env)...), NoUnset: true}
		for _, assignment := range call.Assigns {
			if startupVariable(assignment.Name.Value) {
				next.ShellEnvChanged = true
				next.ClaudePrefixChanged = next.ClaudePrefixChanged || assignment.Name.Value == "CLAUDE_CODE_SHELL_PREFIX"
			}
			if assignment.Value == nil {
				continue
			}
			cfg.Env = expand.ListEnviron(environment(next.Env)...)
			value, e := expand.Literal(cfg, assignment.Value)
			if e != nil {
				next.Unresolved = true
				delete(next.Env, assignment.Name.Value)
				continue
			}
			key := assignment.Name.Value
			if !authorizationIdentityKey(key) {
				next.Env[key] = value
			}
		}
		// Shell words expand in the parent environment, before prefix assignments.
		cfg.Env = expand.ListEnviron(environment(i.Env)...)
		var argv []string
		unknownCommand := false
		for n, word := range call.Args {
			value, e := expand.Literal(cfg, word)
			if e != nil {
				next.Unresolved = true
				var printed bytes.Buffer
				_ = syntax.NewPrinter().Print(&printed, word)
				value = printed.String()
				if n == 0 {
					unknownCommand = true
				}
			}
			argv = append(argv, value)
		}
		next.Command, next.Args = argv[0], argv[1:]
		if unknownCommand {
			err = fmt.Errorf("実行するコマンド名を安全に確定できません: %s", next.Command)
		}
		next.AuthorizationEnv = EnvMap(environment(i.AuthorizationEnv))
		for key, value := range next.Env {
			next.AuthorizationEnv[key] = value
		}
		unwrapped, e := unwrapCommand(next)
		commands = append(commands, unwrapped)
		if e != nil {
			err = e
		}
		if e := commandScript(unwrapped, redirections[call], &commands); e != nil {
			err = e
		}

		return true
	})
	return commands, err
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
		value, e := expand.Literal(&expand.Config{Env: expand.ListEnviron(environment(env)...), NoUnset: true}, word)
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

func lineVerdict(c Config, i Input, line string) (Input, Verdict, error) {
	i.GuardCommands = c.Commands()
	commands, err := lineCommands(i, line)
	i.RawLine = line
	best := Evaluate(c, Input{Cwd: i.Cwd, LaunchCwd: i.LaunchCwd, Env: i.Env, RawLine: line})
	if best.Decision == "block" {
		return i, best, nil
	}
	if danger(c, i) && i.Env["AI_GUARD_DANGER_WORD_ACK"] != "1" {
		best = result("ask", "danger-word-line", "deploy/publish を含む入力行は確認対象です")
	}
	for _, command := range commands {
		v := Evaluate(c, command)
		if command.Unresolved && v.Decision == "allow" && (has(c.Commands(), filepath.Base(command.Command)) || strings.ContainsAny(command.Command, "$`")) {
			v = result("ask", "shell-line", "展開後のコマンドまたは引数を安全に確定できません")
		}
		if v.Decision == "block" || v.Decision == "ask" && best.Decision == "allow" {
			i, best = command, v
		}
		if best.Decision == "block" {
			return i, best, nil
		}
	}
	if err != nil {
		if i.AuthorizationEnv == nil && len(commands) > 0 {
			i.AuthorizationEnv = commands[0].AuthorizationEnv
		}
		return i, result("ask", "shell-line", err.Error()), err
	}
	return i, best, nil
}
