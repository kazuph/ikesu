package guard

import (
	"mvdan.cc/sh/v3/syntax"
	"strings"
)

func descriptorTarget(path string) bool {
	if path == "-" {
		return true
	}
	path = strings.TrimSuffix(path, "-")
	if path == "" {
		return false
	}
	for _, r := range path {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
func setChangesGuard(args []string) bool {
	for n := 0; n < len(args); n++ {
		a := args[n]
		if a == "--" || len(a) < 2 || a[0] != '-' && a[0] != '+' {
			break
		}
		if a == "-o" || a == "+o" {
			if n+1 < len(args) {
				n++
				name := strings.TrimPrefix(strings.ToLower(strings.ReplaceAll(args[n], "_", "")), "no")
				if has([]string{"functrace", "errtrace", "debugbeforecmd", "extdebug", "localtraps", "localoptions"}, name) {
					return true
				}
			}
		} else if !strings.HasPrefix(a, "--") && strings.ContainsAny(a[1:], "ET") {
			return true
		}
	}
	return false
}
func treeChangesDirectory(tree *syntax.File) bool {
	changed := false
	syntax.Walk(tree, func(node syntax.Node) bool {
		if call, ok := node.(*syntax.CallExpr); ok {
			for _, word := range call.Args {
				name := word.Lit()
				if has([]string{"command", "builtin", "-p", "--"}, name) {
					continue
				}
				if has([]string{"cd", "pushd", "popd"}, name) {
					changed = true
				}
				break
			}
		}
		return true
	})
	return changed
}

// Distinguish interpreter flags from an actual script path or explicit code.
// Unknown option/value grammars stay uncertain rather than treating an option's
// value as the script filename.
func interpreterHasProgram(cmd string, args []string) bool {
	for n := 0; n < len(args); n++ {
		a := args[n]
		if a == "-" {
			return false
		}
		if a == "--" {
			return n+1 < len(args) && args[n+1] != "-"
		}
		if !strings.HasPrefix(a, "-") {
			return true
		}
		if has([]string{"-c", "-e", "--eval", "-m"}, a) || cmd == "node" && a == "-p" {
			return n+1 < len(args)
		}
		if strings.HasPrefix(a, "--eval=") {
			return true
		}
		var takesValue, noValue []string
		switch cmd {
		case "python", "python2", "python3":
			takesValue = []string{"-W", "-X", "--check-hash-based-pycs"}
			noValue = []string{"-u", "-B", "-b", "-bb", "-E", "-I", "-O", "-OO", "-P", "-q", "-s", "-S", "-v", "-x"}
		case "perl":
			takesValue = []string{"-I", "-M", "-m", "-F"}
			noValue = []string{"-w", "-W", "-T", "-n", "-p"}
			if strings.Contains(a[1:], "e") && !strings.HasPrefix(a, "--") {
				return n+1 < len(args)
			}
		case "ruby":
			takesValue = []string{"-I", "-r", "-E"}
			noValue = []string{"-w", "-W0", "-W1", "-W2", "-n", "-p"}
		case "node":
			takesValue = []string{"-r", "--require", "--import", "--input-type"}
			noValue = []string{"--no-warnings", "--enable-source-maps", "--experimental-strip-types"}
		case "osascript":
			takesValue = []string{"-l", "-s"}
			noValue = []string{"-i"}
		case "fish":
			takesValue = []string{"-C", "--init-command"}
			noValue = []string{"--no-config", "-N", "-i", "--interactive"}
		}
		if has(takesValue, a) {
			if n+1 >= len(args) {
				return false
			}
			n++
			continue
		}
		if has(noValue, a) || strings.HasPrefix(a, "--") && strings.Contains(a, "=") {
			continue
		}
		return false
	}
	return false
}
