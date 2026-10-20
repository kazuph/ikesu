package guard

import (
	"fmt"
	"strings"
)

func startupVariable(name string) bool {
	return has([]string{"BASH_ENV", "ZDOTDIR", "ENV", "CLAUDE_CODE_SHELL_PREFIX"}, name)
}

// Prefix options change the child environment, not the environment used by
// the caller shell to expand its already parsed words.
func envArguments(i *Input, args []string) ([]string, error) {
	for len(args) > 0 {
		option := args[0]
		switch {
		case option == "--":
			return args[1:], nil
		case option == "-i" || option == "--ignore-environment" || option == "-":
			i.Env = map[string]string{}
			i.ShellEnvChanged = true
			i.ClaudePrefixChanged = true
			args = args[1:]
		case option == "-u" || option == "--unset":
			if len(args) < 2 {
				return nil, prefixError("env")
			}
			key := args[1]
			delete(i.Env, key)
			i.ShellEnvChanged = i.ShellEnvChanged || startupVariable(key)
			i.ClaudePrefixChanged = i.ClaudePrefixChanged || key == "CLAUDE_CODE_SHELL_PREFIX"
			args = args[2:]
		case strings.HasPrefix(option, "--unset=") || strings.HasPrefix(option, "-u") && len(option) > 2:
			key := strings.TrimPrefix(option, "--unset=")
			if key == option {
				key = option[2:]
			}
			delete(i.Env, key)
			i.ShellEnvChanged = i.ShellEnvChanged || startupVariable(key)
			i.ClaudePrefixChanged = i.ClaudePrefixChanged || key == "CLAUDE_CODE_SHELL_PREFIX"
			args = args[1:]
		case option == "-S" || option == "--split-string" || strings.HasPrefix(option, "--split-string=") || strings.HasPrefix(option, "-S") && len(option) > 2:
			var split string
			if option == "-S" || option == "--split-string" {
				if len(args) < 2 {
					return nil, prefixError("env")
				}
				split, args = args[1], args[2:]
			} else if strings.HasPrefix(option, "-S") {
				split, args = option[2:], args[1:]
			} else {
				split, args = strings.TrimPrefix(option, "--split-string="), args[1:]
			}
			// env -S has its own expansion language. Do not approximate it with
			// shell expansion when the split string depends on variables.
			if strings.ContainsAny(split, "$`") {
				return nil, fmt.Errorf("env -S の子環境で展開する内容を確定できません")
			}
			words, e := splitPrefix(split, i.Env)
			if e != nil {
				return nil, e
			}
			args = append(words, args...)
		case strings.Contains(option, "=") && !strings.HasPrefix(option, "-"):
			key, value, _ := strings.Cut(option, "=")
			i.Env[key] = value
			i.ShellEnvChanged = i.ShellEnvChanged || startupVariable(key)
			i.ClaudePrefixChanged = i.ClaudePrefixChanged || key == "CLAUDE_CODE_SHELL_PREFIX"
			args = args[1:]
		case strings.HasPrefix(option, "-"):
			return nil, prefixError("env")
		default:
			return args, nil
		}
	}
	return args, nil
}
